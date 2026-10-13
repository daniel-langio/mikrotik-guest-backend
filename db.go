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

func expireSessionByMAC(db *sql.DB, mac string) error {
	_, err := db.Exec(
		`UPDATE pending_sessions SET status = 'expired' WHERE mac_address = $1 AND status = 'approved'`,
		mac,
	)
	return err
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
