package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// device_history is an append-only event log: each row snapshots the session
// (device, network, payment) as it was when the event happened.

const deviceCols = `session_id, mac_address, ip_address, username, router, os, user_agent, hostname, vendor_class, provider, psp_ref, amount, duration`

// logDevice records an event for a session that still exists.
func logDevice(db *sql.DB, id int, event, actor string) {
	db.Exec(`INSERT INTO device_history (event, actor, `+deviceCols+`)
		SELECT $2, $3, id, mac_address, ip_address, username, router, os, user_agent, hostname, vendor_class, provider, psp_ref, amount, duration
		FROM pending_sessions WHERE id = $1`, id, event, actor)
}

// logPendingDeleted records 'deleted' for one pending session (id > 0) or all
// of them (id == 0), before the rows are removed.
func logPendingDeleted(db *sql.DB, id int, actor string) {
	db.Exec(`INSERT INTO device_history (event, actor, `+deviceCols+`)
		SELECT 'deleted', $2, id, mac_address, ip_address, username, router, os, user_agent, hostname, vendor_class, provider, psp_ref, amount, duration
		FROM pending_sessions WHERE status = 'pending' AND ($1 = 0 OR id = $1)`, id, actor)
}

type DeviceEvent struct {
	At                                       time.Time
	Event, Actor                             string
	SessionID                                int
	MAC, IP, Username, Router, OS, UserAgent string
	Hostname, VendorClass                    string
	Provider, Ref, Duration                  string
	Amount                                   int
}

// listDeviceEvents returns newest first; limit <= 0 means everything.
func listDeviceEvents(db *sql.DB, limit int) ([]DeviceEvent, error) {
	q := `SELECT at, event, actor, COALESCE(session_id, 0), mac_address, COALESCE(ip_address, ''), COALESCE(username, ''),
		COALESCE(router, ''), COALESCE(os, ''), COALESCE(user_agent, ''), COALESCE(hostname, ''), COALESCE(vendor_class, ''), COALESCE(provider, ''), COALESCE(psp_ref, ''),
		COALESCE(amount, 0), COALESCE(duration, '') FROM device_history ORDER BY at DESC, id DESC`
	var args []any
	if limit > 0 {
		q += ` LIMIT $1`
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceEvent
	for rows.Next() {
		var e DeviceEvent
		if err := rows.Scan(&e.At, &e.Event, &e.Actor, &e.SessionID, &e.MAC, &e.IP, &e.Username, &e.Router, &e.OS,
			&e.UserAgent, &e.Hostname, &e.VendorClass, &e.Provider, &e.Ref, &e.Amount, &e.Duration); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (a *App) adminDevices(w http.ResponseWriter, r *http.Request) {
	events, err := listDeviceEvents(a.db, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_devices.html", events)
}

func writeDeviceCSV(w io.Writer, events []DeviceEvent) error {
	c := csv.NewWriter(w)
	c.Write([]string{"at", "event", "actor", "session_id", "mac", "ip", "username", "router", "os", "user_agent", "hostname", "vendor_class", "provider", "ref", "amount", "duration"})
	for _, e := range events {
		c.Write([]string{e.At.UTC().Format(time.RFC3339), e.Event, e.Actor, strconv.Itoa(e.SessionID), e.MAC, e.IP, e.Username,
			e.Router, e.OS, e.UserAgent, e.Hostname, e.VendorClass, e.Provider, e.Ref, strconv.Itoa(e.Amount), e.Duration})
	}
	c.Flush()
	return c.Error()
}

func (a *App) adminDevicesCSV(w http.ResponseWriter, r *http.Request) {
	events, err := listDeviceEvents(a.db, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="devices-%s.csv"`, time.Now().Format("2006-01-02")))
	writeDeviceCSV(w, events)
}
