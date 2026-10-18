package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// The collector records, for every consenting, currently-approved device, the
// connections the router sees it make: protocol, destination IP and port,
// domain name (from the router's DNS cache, best effort) and bytes each way.
// It reads the router's connection-tracking table, so it sees metadata only -
// never page contents (HTTPS stays encrypted).

type Activity struct {
	Key, Proto, DstIP, Domain string
	DstPort                   int
	Up, Down                  int64
}

// activityFor picks the connections originated by ip. The key identifies a
// connection while it lives (ponytail: a reused src port to the same peer
// merges into one row; fine for stats).
func activityFor(conns []Conn, ip string, names map[string]string) []Activity {
	var out []Activity
	for _, c := range conns {
		if c.SrcIP != ip {
			continue
		}
		out = append(out, Activity{
			Key:   fmt.Sprintf("%s|%d|%s|%d", c.Proto, c.SrcPort, c.DstIP, c.DstPort),
			Proto: c.Proto, DstIP: c.DstIP, DstPort: c.DstPort, Domain: names[c.DstIP],
			Up: c.Up, Down: c.Down,
		})
	}
	return out
}

type collectSession struct {
	ID                        int
	MAC, IP, Username, Router string
}

func listCollectable(db *sql.DB) ([]collectSession, error) {
	rows, err := db.Query(`SELECT s.id, s.mac_address, s.ip_address, s.username, s.router FROM pending_sessions s
		WHERE s.status = 'approved' AND COALESCE(s.ip_address, '') <> ''
		AND EXISTS (SELECT 1 FROM consents c WHERE c.session_id = s.id AND c.withdrawn_at IS NULL)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collectSession
	for rows.Next() {
		var s collectSession
		if err := rows.Scan(&s.ID, &s.MAC, &s.IP, &s.Username, &s.Router); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func upsertActivity(db *sql.DB, s collectSession, a Activity) error {
	_, err := db.Exec(`INSERT INTO activity_log (session_id, mac_address, username, router, proto, dst_ip, dst_port, domain, bytes_up, bytes_down, conn_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (session_id, conn_key) DO UPDATE SET last_seen = now(),
		  bytes_up = GREATEST(activity_log.bytes_up, EXCLUDED.bytes_up),
		  bytes_down = GREATEST(activity_log.bytes_down, EXCLUDED.bytes_down),
		  domain = COALESCE(NULLIF(activity_log.domain, ''), EXCLUDED.domain)`,
		s.ID, s.MAC, s.Username, s.Router, a.Proto, a.DstIP, a.DstPort, a.Domain, a.Up, a.Down, a.Key)
	return err
}

// collectOnce samples every router that has consenting devices on it.
func (a *App) collectOnce() {
	sessions, err := listCollectable(a.db)
	if err != nil || len(sessions) == 0 {
		return
	}
	byRouter := map[string][]collectSession{}
	for _, s := range sessions {
		byRouter[s.Router] = append(byRouter[s.Router], s)
	}
	for router, ss := range byRouter {
		mt := a.router(router)
		conns, err := mt.Connections()
		if err != nil {
			log.Printf("collector: %s: %v", router, err)
			continue
		}
		names, _ := mt.DNSNames()
		for _, s := range ss {
			for _, act := range activityFor(conns, s.IP, names) {
				upsertActivity(a.db, s, act)
			}
		}
	}
}

func purgeActivity(db *sql.DB, days int) {
	db.Exec(`DELETE FROM activity_log WHERE last_seen < now() - $1::interval`, fmt.Sprintf("%d days", days))
}

// collectLoop samples every COLLECT_INTERVAL seconds (default 30) and purges
// rows past the retention period (the notice promises it) once an hour.
func (a *App) collectLoop() {
	interval := 30 * time.Second
	if n, err := strconv.Atoi(os.Getenv("COLLECT_INTERVAL")); err == nil && n > 0 {
		interval = time.Duration(n) * time.Second
	}
	lastPurge := time.Time{}
	for range time.Tick(interval) {
		a.collectOnce()
		if time.Since(lastPurge) > time.Hour {
			purgeActivity(a.db, retentionDays())
			lastPurge = time.Now()
		}
	}
}

type ActivityRow struct {
	LastSeen, FirstSeen          time.Time
	Username, MAC, Router, Proto string
	DstIP, Domain                string
	DstPort                      int
	Up, Down                     int64
}

func listActivity(db *sql.DB, limit int) ([]ActivityRow, error) {
	q := `SELECT last_seen, first_seen, COALESCE(username, ''), mac_address, router, COALESCE(proto, ''), COALESCE(dst_ip, ''),
		COALESCE(domain, ''), COALESCE(dst_port, 0), bytes_up, bytes_down FROM activity_log ORDER BY last_seen DESC, id DESC`
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
	var out []ActivityRow
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.LastSeen, &r.FirstSeen, &r.Username, &r.MAC, &r.Router, &r.Proto, &r.DstIP, &r.Domain, &r.DstPort, &r.Up, &r.Down); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *App) adminActivity(w http.ResponseWriter, r *http.Request) {
	rows, err := listActivity(a.db, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_activity.html", rows)
}

func (a *App) adminActivityCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := listActivity(a.db, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="activity-%s.csv"`, time.Now().Format("2006-01-02")))
	c := csv.NewWriter(w)
	c.Write([]string{"first_seen", "last_seen", "username", "mac", "router", "proto", "dst_ip", "domain", "dst_port", "bytes_up", "bytes_down"})
	for _, r := range rows {
		c.Write([]string{r.FirstSeen.UTC().Format(time.RFC3339), r.LastSeen.UTC().Format(time.RFC3339), r.Username, r.MAC, r.Router,
			r.Proto, r.DstIP, r.Domain, strconv.Itoa(r.DstPort), strconv.FormatInt(r.Up, 10), strconv.FormatInt(r.Down, 10)})
	}
	c.Flush()
}

// humanBytes for the admin list.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
