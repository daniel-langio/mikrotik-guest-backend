package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
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
}

func openDB() (*sql.DB, error) {
	return sql.Open("pgx", os.Getenv("DATABASE_URL"))
}

func insertPending(db *sql.DB, mac, ip, username, router string) (int, error) {
	var id int
	err := db.QueryRow(
		`INSERT INTO pending_sessions (mac_address, ip_address, username, router) VALUES ($1, $2, $3, $4) RETURNING id`,
		mac, ip, username, router,
	).Scan(&id)
	return id, err
}

func listPending(db *sql.DB) ([]PendingSession, error) {
	rows, err := db.Query(
		`SELECT id, mac_address, ip_address, username, status, duration, router, created_at, approved_at
		 FROM pending_sessions WHERE status = 'pending' ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []PendingSession
	for rows.Next() {
		var s PendingSession
		if err := rows.Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.Router, &s.CreatedAt, &s.ApprovedAt); err != nil {
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
}

func listApproved(db *sql.DB) ([]ApprovedSession, error) {
	rows, err := db.Query(
		`SELECT id, username, mac_address, router, approved_at + duration::interval AS expires_at
		 FROM pending_sessions WHERE status = 'approved' ORDER BY approved_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []ApprovedSession
	for rows.Next() {
		var s ApprovedSession
		if err := rows.Scan(&s.ID, &s.Username, &s.MACAddress, &s.Router, &s.ExpiresAt); err != nil {
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
		`UPDATE pending_sessions SET status = 'approved', duration = $1, approved_at = now() WHERE id = $2`,
		duration, id,
	)
	return err
}

func createAdminSession(db *sql.DB, token string, ttl time.Duration) error {
	_, err := db.Exec(
		`INSERT INTO admin_sessions (token, expires_at) VALUES ($1, now() + $2::interval)`,
		token, fmt.Sprintf("%d seconds", int(ttl.Seconds())),
	)
	return err
}

func validAdminSession(db *sql.DB, token string) bool {
	var exists bool
	_ = db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM admin_sessions WHERE token = $1 AND expires_at > now())`,
		token,
	).Scan(&exists)
	return exists
}

func deleteAdminSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM admin_sessions WHERE token = $1`, token)
	return err
}
