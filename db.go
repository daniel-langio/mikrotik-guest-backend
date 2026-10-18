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

func insertPending(db *sql.DB, mac, ip, username, router, os string) (int, error) {
	var id int
	err := db.QueryRow(
		`INSERT INTO pending_sessions (mac_address, ip_address, username, router, os) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		mac, ip, username, router, os,
	).Scan(&id)
	return id, err
}

func listPending(db *sql.DB) ([]PendingSession, error) {
	rows, err := db.Query(
		`SELECT id, mac_address, ip_address, username, status, duration, router, created_at, approved_at, COALESCE(os, '')
		 FROM pending_sessions WHERE status = 'pending' ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []PendingSession
	for rows.Next() {
		var s PendingSession
		if err := rows.Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.Router, &s.CreatedAt, &s.ApprovedAt, &s.OS); err != nil {
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
		 RETURNING router, mac_address`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct{ Router, MAC string }
	for rows.Next() {
		var e struct{ Router, MAC string }
		if err := rows.Scan(&e.Router, &e.MAC); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type ApprovedSession struct {
	ID         int
	Username   string
	MACAddress string
	Router     string
	ExpiresAt  time.Time
	OS         string
}

func listApproved(db *sql.DB) ([]ApprovedSession, error) {
	rows, err := db.Query(
		`SELECT id, username, mac_address, router, approved_at + duration::interval AS expires_at, COALESCE(os, '')
		 FROM pending_sessions WHERE status = 'approved' ORDER BY approved_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []ApprovedSession
	for rows.Next() {
		var s ApprovedSession
		if err := rows.Scan(&s.ID, &s.Username, &s.MACAddress, &s.Router, &s.ExpiresAt, &s.OS); err != nil {
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

func approveSession(db *sql.DB, id int, duration string) error {
	_, err := db.Exec(
		`UPDATE pending_sessions SET status = 'approved', duration = $1, approved_at = now() WHERE id = $2 AND status = 'pending'`,
		duration, id,
	)
	return err
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

func deletePending(db *sql.DB, id int) error {
	_, err := db.Exec(`DELETE FROM pending_sessions WHERE id = $1 AND status = 'pending'`, id)
	return err
}

func clearPending(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM pending_sessions WHERE status = 'pending'`)
	return err
}

// Open portal: while the "open_duration" setting exists, every new guest is
// approved automatically for that long (HH:MM:SS).
func getOpenDuration(db *sql.DB) (string, bool) {
	var d string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = 'open_duration'`).Scan(&d)
	return d, err == nil
}

func setOpenDuration(db *sql.DB, d string) error {
	_, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES ('open_duration', $1)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, d)
	return err
}

func clearOpen(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM settings WHERE key = 'open_duration'`)
	return err
}

// setOS refreshes a session's OS when a later request tells us more (e.g. the
// real browser after the captive-portal mini browser).
func setOS(db *sql.DB, id int, os string) {
	if os != "" {
		db.Exec(`UPDATE pending_sessions SET os = $1 WHERE id = $2`, os, id)
	}
}
