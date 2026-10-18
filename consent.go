package main

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
)

// Free (open-portal) access comes with activity recording. The guest page
// carries one line - "by using our service you accept our confidentiality
// policy" - linking to the full policy (/privacy); using the free service is
// the acceptance, stored per session as an 'implied' consent. Collection
// (collector.go) only covers sessions with a live, un-withdrawn consent, and
// the policy page lets a guest withdraw.
//
// Keep the policy (dict consent.* keys) in sync with what collector.go records.
const policyVersion = "2026-10-18"

func retentionDays() int {
	if n, err := strconv.Atoi(os.Getenv("RETENTION_DAYS")); err == nil && n > 0 {
		return n
	}
	return 90
}

func hasConsent(db *sql.DB, sessionID int) bool {
	var ok bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM consents WHERE session_id = $1 AND withdrawn_at IS NULL)`, sessionID).Scan(&ok)
	return ok
}

func recordConsent(db *sql.DB, sess PendingSession, lang, userAgent, method string) error {
	_, err := db.Exec(`INSERT INTO consents (session_id, mac_address, policy_version, lang, ip_address, user_agent, method)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, sess.ID, sess.MACAddress, policyVersion, lang, sess.IPAddress, userAgent, method)
	if err == nil {
		logDevice(db, sess.ID, "consent", "guest")
	}
	return err
}

func (a *App) privacy(w http.ResponseWriter, r *http.Request) {
	d := map[string]any{"Retention": retentionDays(), "Contact": os.Getenv("PRIVACY_CONTACT"), "Version": policyVersion}
	if id, err := strconv.Atoi(r.URL.Query().Get("s")); err == nil {
		if sess, err := getSession(a.db, id); err == nil && sess.MACAddress == r.URL.Query().Get("mac") && hasConsent(a.db, id) {
			d["SessionID"], d["MAC"] = id, sess.MACAddress
		}
	}
	render(w, r, "privacy.html", d)
}

// consentWithdraw ends the guest's consent: collection stops, their activity
// rows are deleted and the free access ends (it was conditional on consent).
func (a *App) consentWithdraw(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.FormValue("s"))
	sess, err := getSession(a.db, id)
	if err != nil || sess.MACAddress != r.FormValue("mac") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	a.db.Exec(`UPDATE consents SET withdrawn_at = now() WHERE session_id = $1 AND withdrawn_at IS NULL`, id)
	a.db.Exec(`DELETE FROM activity_log WHERE session_id = $1`, id)
	logDevice(a.db, id, "withdrawn", "guest")
	if sess.Status == "approved" {
		a.db.Exec(`UPDATE pending_sessions SET status = 'expired' WHERE id = $1`, id)
		a.router(sess.Router).DeauthorizeDevice(sess.MACAddress)
	}
	render(w, r, "privacy.html", map[string]any{"Withdrawn": true, "Retention": retentionDays(), "Contact": os.Getenv("PRIVACY_CONTACT"), "Version": policyVersion})
}
