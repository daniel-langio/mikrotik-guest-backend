package main

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
)

// Free (open-portal) access is given in exchange for recording the guest's
// activity. The guest must tick an explicit box after reading exactly what is
// collected; the consent is stored per session and versioned. Collection
// (collector.go) only ever covers sessions with a live, un-withdrawn consent.
//
// Keep the notice (dict consent.* keys) in sync with what collector.go records.
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

func recordConsent(db *sql.DB, sess PendingSession, lang, userAgent string) error {
	_, err := db.Exec(`INSERT INTO consents (session_id, mac_address, policy_version, lang, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`, sess.ID, sess.MACAddress, policyVersion, lang, sess.IPAddress, userAgent)
	if err == nil {
		logDevice(db, sess.ID, "consent", "guest")
	}
	return err
}

// needsConsent: the portal is open (free access) and this guest hasn't
// consented yet (and hasn't chosen to pay instead).
func (a *App) needsConsent(sess PendingSession, hasClaim bool) bool {
	if sess.Status != "pending" || hasClaim {
		return false
	}
	_, _, open := getOpen(a.db)
	return open && !hasConsent(a.db, sess.ID)
}

// welcomeConsent records the guest's consent and, if the portal is still
// open, connects them.
func (a *App) welcomeConsent(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	sess, err := getSession(a.db, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.FormValue("agree") == "1" && sess.Status == "pending" && !hasConsent(a.db, id) {
		if err := recordConsent(a.db, sess, langOf(r), r.UserAgent()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	d := a.payData(sess)
	d["Accepted"] = a.autoGrant(sess)
	render(w, r, "pay.html", d)
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
