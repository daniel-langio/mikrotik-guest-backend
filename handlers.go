package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

// buildVersion busts stale static-asset caches on mobile browsers, which
// have no equivalent to a desktop hard-refresh - appending it to every
// asset URL guarantees a fresh fetch after each deploy, no user action
// needed (Cache-Control: no-cache alone still needs a revalidation
// round-trip the client isn't guaranteed to make).
var buildVersion = fmt.Sprintf("%d", time.Now().Unix())

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"v": func() string { return buildVersion },
}).ParseGlob("templates/*.html"))

// Matches MikroTik's limit-uptime format (HH:MM:SS, hours uncapped). Enforced
// server-side since a malformed value would break the duration::interval
// cast in sweepExpiredSessions for every approved row, not just this one.
var durationFormat = regexp.MustCompile(`^\d{1,4}:\d{2}:\d{2}$`)

// atoiOrZero parses an optional numeric form field. ok is false when s is
// empty, so callers can tell "not provided" apart from "provided as 0".
func atoiOrZero(s string) (n int, ok bool) {
	if s == "" {
		return 0, false
	}
	n, _ = strconv.Atoi(s)
	return n, true
}

func redirectURL() string {
	if u := os.Getenv("REDIRECT_URL"); u != "" {
		return u
	}
	return "https://en.wikipedia.org/wiki/Aphrodite"
}

type App struct {
	db            *sql.DB
	routers       map[string]MikroTikClient
	defaultRouter string
	ifay          *Ifay // nil = manual admin approval only
}

// router returns the client for name, falling back to the default router if
// name is empty or unknown (e.g. an older login.html without &router=).
func (a *App) router(name string) MikroTikClient {
	if mt, ok := a.routers[name]; ok {
		return mt
	}
	return a.routers[a.defaultRouter]
}

func randSuffix() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func (a *App) welcome(w http.ResponseWriter, r *http.Request) {
	mac := r.URL.Query().Get("mac")
	ip := r.URL.Query().Get("ip")
	router := r.URL.Query().Get("router")
	if router == "" {
		router = a.defaultRouter
	}

	// Reuse this device's existing pending/approved session on a refresh,
	// rather than minting a new GUEST-xxxx account and orphaning whatever
	// row the admin is currently looking at.
	if sess, err := getOpenSessionByMAC(a.db, mac); err == nil {
		tmpl.ExecuteTemplate(w, "welcome.html", a.payData(sess))
		return
	} else if err != sql.ErrNoRows {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	username := "GUEST-" + randSuffix()
	id, err := insertPending(a.db, mac, ip, username, router)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "welcome.html", a.payData(PendingSession{ID: id, Username: username}))
}

// welcomeStatus is polled by the guest's own welcome page. Once an admin
// approves the session (or the guest's Mobile Money claim verifies), it swaps
// in an "accepted" message, which then triggers the redirect; until then it
// answers 204 so nothing on the page changes.
func (a *App) welcomeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	sess, err := getSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if sess.Status == "approved" || a.checkClaim(sess) {
		d := a.payData(sess)
		d["Accepted"] = true
		tmpl.ExecuteTemplate(w, "pay.html", d)
		return
	}
	w.WriteHeader(http.StatusNoContent) // htmx leaves #pay untouched on 204
}

// welcomeRedirect is hit a couple of seconds after the "accepted" feedback is
// shown, so the guest sees the confirmation before leaving the portal.
func (a *App) welcomeRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", redirectURL())
}

func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tmpl.ExecuteTemplate(w, "admin.html", map[string]any{"Username": u.Name, "IsAdmin": u.IsAdmin})
}

func (a *App) adminPending(w http.ResponseWriter, r *http.Request) {
	sessions, err := listPending(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "admin_pending.html", sessions)
}

func (a *App) adminActive(w http.ResponseWriter, r *http.Request) {
	// Opportunistic sweep, piggybacked on the dashboard's existing 5s poll.
	// Approval uses an ip-binding bypass with no RouterOS-side time limit, so
	// our own DB (approved_at + duration) is the only thing tracking expiry.
	if expired, err := sweepExpiredSessions(a.db); err == nil {
		for _, e := range expired {
			a.router(e.Router).DeauthorizeDevice(e.MAC)
		}
	}

	sessions, err := listApproved(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "admin_active.html", sessions)
}

// adminDisconnect kicks a device off the router and removes its session
// row outright (not just marking it expired - the admin asked to delete it).
func (a *App) adminDisconnect(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	router, mac, err := deleteSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := a.router(router).DeauthorizeDevice(mac); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	sessions, err := listApproved(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "admin_active.html", sessions)
}

func (a *App) approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	duration := r.FormValue("duration")
	h, hOK := atoiOrZero(r.FormValue("custom_h"))
	m, mOK := atoiOrZero(r.FormValue("custom_m"))
	s, sOK := atoiOrZero(r.FormValue("custom_s"))
	if hOK || mOK || sOK {
		duration = fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	if !durationFormat.MatchString(duration) {
		http.Error(w, "duration must be HH:MM:SS", http.StatusBadRequest)
		return
	}

	sess, err := getSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := a.router(sess.Router).AuthorizeDevice(sess.MACAddress); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := approveSession(a.db, id, duration); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessions, err := listPending(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "admin_pending.html", sessions)
}
