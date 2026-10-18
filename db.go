package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type PendingSession struct {
	ID         int
	MACAddress string
	IPAddress  string
	Username   string
	Status     string
	Duration   sql.NullString
	Router     string
	CreatedAt  time.Time
	ApprovedAt sql.NullTime
	OS         string // from the portal request's User-Agent, see deviceOS
	Hostname   string // from the router's DHCP lease
}

// openDB uses the simple protocol: behind a transaction-mode pooler
// (pgbouncer, Supabase) pgx's cached prepared statements collide across
// pooled connections ("prepared statement name is already in use").
func openDB() (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return stdlib.OpenDB(*cfg), nil
}

func insertPending(db *sql.DB, mac, ip, username, router, os, userAgent string) (int, error) {
	var id int
	err := db.QueryRow(
		`INSERT INTO pending_sessions (mac_address, ip_address, username, router, os, user_agent) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		mac, ip, username, router, os, userAgent,
	).Scan(&id)
	if err == nil {
		logDevice(db, id, "pending", "")
	}
	return id, err
}

func listPending(db *sql.DB) ([]PendingSession, error) {
	rows, err := db.Query(
		`SELECT id, mac_address, ip_address, username, status, duration, router, created_at, approved_at, COALESCE(os, ''), COALESCE(hostname, '')
		 FROM pending_sessions WHERE status = 'pending' ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []PendingSession
	for rows.Next() {
		var s PendingSession
		if err := rows.Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.Router, &s.CreatedAt, &s.ApprovedAt, &s.OS, &s.Hostname); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// getOpenSessionByMAC finds this device's existing pending/approved session,
// if any - so a page refresh reuses it instead of minting a new GUEST-xxxx
// account and orphaning whatever the admin is looking at.
func getOpenSessionByMAC(db *sql.DB, mac string) (PendingSession, error) {
	var s PendingSession
	err := db.QueryRow(
		`SELECT id, mac_address, ip_address, username, status, duration, router, created_at, approved_at
		 FROM pending_sessions WHERE mac_address = $1 AND status IN ('pending', 'approved')
		 ORDER BY created_at DESC LIMIT 1`,
		mac,
	).Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.Router, &s.CreatedAt, &s.ApprovedAt)
	return s, err
}

// sweepExpiredSessions flips any 'approved' row whose duration has elapsed to
// 'expired' and returns the (router, mac) pairs so the caller can revoke
// access on the right router. duration is stored in MikroTik's "HH:MM:SS"
// format, which Postgres parses directly as an interval literal.
func sweepExpiredSessions(db *sql.DB) ([]struct{ Router, MAC string }, error) {
	rows, err := db.Query(
		`UPDATE pending_sessions SET status = 'expired'
		 WHERE status = 'approved' AND approved_at + duration::interval <= now()
		 RETURNING router, mac_address, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct{ Router, MAC string }
	var ids []int
	for rows.Next() {
		var e struct{ Router, MAC string }
		var id int
		if err := rows.Scan(&e.Router, &e.MAC, &id); err != nil {
			return nil, err
		}
		out = append(out, e)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, id := range ids {
		logDevice(db, id, "expired", "expiry")
	}
	return out, nil
}

type ApprovedSession struct {
	ID         int
	Username   string
	MACAddress string
	Router     string
	ExpiresAt  time.Time
	OS         string
	Hostname   string
	State      string // online | offline | unknown, from the router, see activeSessions
}

func listApproved(db *sql.DB) ([]ApprovedSession, error) {
	rows, err := db.Query(
		`SELECT id, username, mac_address, router, approved_at + duration::interval AS expires_at, COALESCE(os, ''), COALESCE(hostname, '')
		 FROM pending_sessions WHERE status = 'approved' ORDER BY approved_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []ApprovedSession
	for rows.Next() {
		var s ApprovedSession
		if err := rows.Scan(&s.ID, &s.Username, &s.MACAddress, &s.Router, &s.ExpiresAt, &s.OS, &s.Hostname); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// deleteSession removes a row outright and returns its router+MAC, so the
// caller can revoke router access too.
func deleteSession(db *sql.DB, id int) (router, mac string, err error) {
	err = db.QueryRow(`DELETE FROM pending_sessions WHERE id = $1 RETURNING router, mac_address`, id).Scan(&router, &mac)
	return router, mac, err
}

func getSession(db *sql.DB, id int) (PendingSession, error) {
	var s PendingSession
	err := db.QueryRow(
		`SELECT id, mac_address, ip_address, username, status, duration, router, created_at, approved_at
		 FROM pending_sessions WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.Router, &s.CreatedAt, &s.ApprovedAt)
	return s, err
}

// approveSession flips a pending session to approved and logs it. actor says
// who/what approved: "admin:<name>", "payment" or "open". A session that is
// no longer pending (a concurrent poll got there first) is left alone and
// not logged twice.
func approveSession(db *sql.DB, id int, duration, actor string) error {
	res, err := db.Exec(
		`UPDATE pending_sessions SET status = 'approved', duration = $1, approved_at = now() WHERE id = $2 AND status = 'pending'`,
		duration, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		logDevice(db, id, "approved", actor)
	}
	return nil
}

func createAdminSession(db *sql.DB, token string, ttl time.Duration, username string, isAdmin bool) error {
	_, err := db.Exec(
		`INSERT INTO admin_sessions (token, expires_at, username, is_admin) VALUES ($1, now() + $2::interval, $3, $4)`,
		token, fmt.Sprintf("%d seconds", int(ttl.Seconds())), username, isAdmin,
	)
	return err
}

func getAdminSession(db *sql.DB, token string) (username string, isAdmin, ok bool) {
	err := db.QueryRow(
		`SELECT username, is_admin FROM admin_sessions WHERE token = $1 AND expires_at > now()`,
		token,
	).Scan(&username, &isAdmin)
	return username, isAdmin, err == nil
}

func deleteAdminSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM admin_sessions WHERE token = $1`, token)
	return err
}

// getClaim returns this session's Mobile Money claim; claimID is "" until filed with ifay.
func getClaim(db *sql.DB, id int) (ref, claimID string, amount int, err error) {
	err = db.QueryRow(
		`SELECT COALESCE(psp_ref, ''), COALESCE(claim_id, ''), COALESCE(amount, 0) FROM pending_sessions WHERE id = $1`,
		id,
	).Scan(&ref, &claimID, &amount)
	return
}

// reserveRef claims (provider, ref) for a pending session before ifay is
// called; the unique index rejects a ref already used by another guest.
// sql.ErrNoRows means the session isn't pending any more.
func reserveRef(db *sql.DB, id int, provider, ref string, amount int) error {
	var got int
	return db.QueryRow(
		`UPDATE pending_sessions SET provider = $1, psp_ref = $2, amount = $3, claim_id = NULL
		 WHERE id = $4 AND status = 'pending' RETURNING id`,
		provider, ref, amount, id,
	).Scan(&got)
}

func setClaimID(db *sql.DB, id int, claimID string) error {
	_, err := db.Exec(`UPDATE pending_sessions SET claim_id = $1 WHERE id = $2`, claimID, id)
	return err
}

func recordClaim(db *sql.DB, ifayID string) {
	db.Exec(`INSERT INTO claims (ifay_id) VALUES ($1) ON CONFLICT DO NOTHING`, ifayID)
}

func clearClaim(db *sql.DB, id int) {
	db.Exec(`UPDATE pending_sessions SET provider = NULL, psp_ref = NULL, amount = NULL, claim_id = NULL WHERE id = $1`, id)
}

func deletePending(db *sql.DB, id int, actor string) error {
	logPendingDeleted(db, id, actor)
	_, err := db.Exec(`DELETE FROM pending_sessions WHERE id = $1 AND status = 'pending'`, id)
	return err
}

func clearPending(db *sql.DB, actor string) error {
	logPendingDeleted(db, 0, actor)
	_, err := db.Exec(`DELETE FROM pending_sessions WHERE status = 'pending'`)
	return err
}

// Open portal: while the "open_duration" setting exists, every new guest is
// approved automatically for that long (HH:MM:SS). An optional "open_until"
// (RFC3339) closes the portal by itself: it is checked on use, and an elapsed
// window is cleared on the spot, so no background job is needed.
func getOpen(db *sql.DB) (duration string, until time.Time, ok bool) {
	var d, u sql.NullString
	err := db.QueryRow(`SELECT (SELECT value FROM settings WHERE key = 'open_duration'),
		(SELECT value FROM settings WHERE key = 'open_until')`).Scan(&d, &u)
	if err != nil || !d.Valid {
		return "", time.Time{}, false
	}
	if u.Valid {
		if until, err = time.Parse(time.RFC3339, u.String); err == nil && !time.Now().Before(until) {
			clearOpen(db)
			return "", time.Time{}, false
		}
	}
	return d.String, until, true
}

// setOpen opens the portal; a zero until means "stay open until closed by hand".
func setOpen(db *sql.DB, duration string, until time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('open_duration', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, duration); err != nil {
		return err
	}
	if until.IsZero() {
		_, err = tx.Exec(`DELETE FROM settings WHERE key = 'open_until'`)
	} else {
		_, err = tx.Exec(`INSERT INTO settings (key, value) VALUES ('open_until', $1)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, until.UTC().Format(time.RFC3339))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func clearOpen(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM settings WHERE key IN ('open_duration', 'open_until')`)
	return err
}

// setDevice refreshes a session's OS/User-Agent when a later request tells us
// more (e.g. the real browser after the captive-portal mini browser).
func setDevice(db *sql.DB, id int, os, userAgent string) {
	if os != "" {
		db.Exec(`UPDATE pending_sessions SET os = $1, user_agent = $2 WHERE id = $3`, os, userAgent, id)
	}
}

// setLease stores what the router's DHCP lease says about a session's device,
// on the session and on its 'pending' history row. The OS is only filled in
// when the browser didn't already tell us.
func setLease(db *sql.DB, id int, l Lease) {
	os := osFromLease(l)
	db.Exec(`UPDATE pending_sessions SET hostname = $1, vendor_class = $2, os = COALESCE(NULLIF(os, ''), NULLIF($3, '')) WHERE id = $4`,
		l.Hostname, l.Vendor, os, id)
	db.Exec(`UPDATE device_history SET hostname = $1, vendor_class = $2, os = COALESCE(NULLIF(os, ''), NULLIF($3, '')) WHERE session_id = $4 AND event = 'pending'`,
		l.Hostname, l.Vendor, os, id)
}
