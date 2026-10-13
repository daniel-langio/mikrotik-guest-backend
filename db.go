package main

import (
	"database/sql"
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
	CreatedAt  time.Time
	ApprovedAt sql.NullTime
}

func openDB() (*sql.DB, error) {
	return sql.Open("pgx", os.Getenv("DATABASE_URL"))
}

func insertPending(db *sql.DB, mac, ip, username string) (int, error) {
	var id int
	err := db.QueryRow(
		`INSERT INTO pending_sessions (mac_address, ip_address, username) VALUES ($1, $2, $3) RETURNING id`,
		mac, ip, username,
	).Scan(&id)
	return id, err
}

func listPending(db *sql.DB) ([]PendingSession, error) {
	rows, err := db.Query(
		`SELECT id, mac_address, ip_address, username, status, duration, created_at, approved_at
		 FROM pending_sessions WHERE status = 'pending' ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []PendingSession
	for rows.Next() {
		var s PendingSession
		if err := rows.Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.CreatedAt, &s.ApprovedAt); err != nil {
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
		`SELECT id, mac_address, ip_address, username, status, duration, created_at, approved_at
		 FROM pending_sessions WHERE mac_address = $1 AND status IN ('pending', 'approved')
		 ORDER BY created_at DESC LIMIT 1`,
		mac,
	).Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.CreatedAt, &s.ApprovedAt)
	return s, err
}

// sweepExpiredSessions flips any 'approved' row whose duration has elapsed to
// 'expired' and returns their MACs, so the caller can revoke router access.
// duration is stored in MikroTik's "HH:MM:SS" format, which Postgres parses
// directly as an interval literal.
func sweepExpiredSessions(db *sql.DB) ([]string, error) {
	rows, err := db.Query(
		`UPDATE pending_sessions SET status = 'expired'
		 WHERE status = 'approved' AND approved_at + duration::interval <= now()
		 RETURNING mac_address`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var macs []string
	for rows.Next() {
		var mac string
		if err := rows.Scan(&mac); err != nil {
			return nil, err
		}
		macs = append(macs, mac)
	}
	return macs, rows.Err()
}

type ApprovedSession struct {
	ID         int
	Username   string
	MACAddress string
	Remaining  string
}

func listApproved(db *sql.DB) ([]ApprovedSession, error) {
	rows, err := db.Query(
		`SELECT id, username, mac_address,
		        to_char(duration::interval - (now() - approved_at), 'HH24:MI:SS') AS remaining
		 FROM pending_sessions WHERE status = 'approved' ORDER BY approved_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []ApprovedSession
	for rows.Next() {
		var s ApprovedSession
		if err := rows.Scan(&s.ID, &s.Username, &s.MACAddress, &s.Remaining); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// deleteSession removes a row outright and returns its MAC, so the caller
// can revoke router access too.
func deleteSession(db *sql.DB, id int) (string, error) {
	var mac string
	err := db.QueryRow(`DELETE FROM pending_sessions WHERE id = $1 RETURNING mac_address`, id).Scan(&mac)
	return mac, err
}

func getSession(db *sql.DB, id int) (PendingSession, error) {
	var s PendingSession
	err := db.QueryRow(
		`SELECT id, mac_address, ip_address, username, status, duration, created_at, approved_at
		 FROM pending_sessions WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.MACAddress, &s.IPAddress, &s.Username, &s.Status, &s.Duration, &s.CreatedAt, &s.ApprovedAt)
	return s, err
}

func approveSession(db *sql.DB, id int, duration string) error {
	_, err := db.Exec(
		`UPDATE pending_sessions SET status = 'approved', duration = $1, approved_at = now() WHERE id = $2`,
		duration, id,
	)
	return err
}
