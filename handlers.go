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
	"strings"
	"time"
)

// buildVersion busts stale static-asset caches on mobile browsers, which
// have no equivalent to a desktop hard-refresh - appending it to every
// asset URL guarantees a fresh fetch after each deploy, no user action
// needed (Cache-Control: no-cache alone still needs a revalidation
// round-trip the client isn't guaranteed to make).
var buildVersion = fmt.Sprintf("%d", time.Now().Unix())

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"v":     func() string { return buildVersion },
	"bytes": humanBytes,
	// placeholders so the templates parse; render() rebinds them per request
	"t":    func(key string, args ...any) string { return key },
	"lang": func() string { return defaultLang },
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

// formDuration reads the admin duration controls (preset select, or custom
// h/m/s which win when filled) and validates the HH:MM:SS format.
func formDuration(r *http.Request) (string, bool) {
	duration := r.FormValue("duration")
	h, hOK := atoiOrZero(r.FormValue("custom_h"))
	m, mOK := atoiOrZero(r.FormValue("custom_m"))
	s, sOK := atoiOrZero(r.FormValue("custom_s"))
	if hOK || mOK || sOK {
		duration = fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return duration, durationFormat.MatchString(duration)
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
	os := deviceOS(r.UserAgent())
	if sess, err := getOpenSessionByMAC(a.db, mac); err == nil {
		setDevice(a.db, sess.ID, os, r.UserAgent())
		d := a.payData(sess)
		d["Accepted"] = a.autoGrant(sess)
		render(w, r, "welcome.html", d)
		return
	} else if err != sql.ErrNoRows {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	username := "GUEST-" + randSuffix()
	id, err := insertPending(a.db, mac, ip, username, router, os, r.UserAgent())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go a.fetchLease(id, router, mac)
	sess := PendingSession{ID: id, MACAddress: mac, Username: username, Status: "pending", Router: router}
	d := a.payData(sess)
	d["Accepted"] = a.autoGrant(sess)
	render(w, r, "welcome.html", d)
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
		render(w, r, "pay.html", d)
		return
	}
	// The page was drawn with or without the consent gate (g); if the portal
	// opened or closed since, reload once so the guest sees the right screen.
	_, claimID, _, _ := getClaim(a.db, sess.ID)
	if sess.Status == "pending" && a.needsConsent(sess, claimID != "") != (r.URL.Query().Get("g") == "1") {
		w.Header().Set("HX-Refresh", "true")
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
	render(w, r, "admin.html", map[string]any{"Username": u.Name, "IsAdmin": u.IsAdmin})
}

func (a *App) adminPending(w http.ResponseWriter, r *http.Request) {
	sessions, err := listPending(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_pending.html", sessions)
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

	sessions, err := a.activeSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_active.html", sessions)
}

// adminDisconnect kicks a device off the router and removes its session
// row outright (not just marking it expired - the admin asked to delete it).
func (a *App) adminDisconnect(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	logDevice(a.db, id, "disconnected", "admin:"+currentUser(r).Name)
	router, mac, err := deleteSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := a.router(router).DeauthorizeDevice(mac); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	sessions, err := a.activeSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_active.html", sessions)
}

func (a *App) approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	duration, ok := formDuration(r)
	if !ok {
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
	if err := approveSession(a.db, id, duration, "admin:"+currentUser(r).Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessions, err := listPending(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_pending.html", sessions)
}

// help is the public "how it works" page: an auto-advancing demo of the pay flow.
func (a *App) help(w http.ResponseWriter, r *http.Request) {
	d := map[string]any{"Price": pricePerUnit, "Mins": minsPerUnit}
	if a.ifay != nil {
		d["Receiver"] = a.ifay.receiver
	}
	render(w, r, "help.html", d)
}

// autoGrant approves a pending session on the spot while the portal is open -
// but only once the guest has consented to activity recording (free access is
// given in exchange for it, see consent.go).
func (a *App) autoGrant(sess PendingSession) bool {
	d, _, open := getOpen(a.db)
	return open && sess.Status == "pending" && hasConsent(a.db, sess.ID) && a.grant(sess, d, "open") == nil
}

func (a *App) renderOpen(w http.ResponseWriter, r *http.Request) {
	d, until, _ := getOpen(a.db)
	data := map[string]any{"Duration": d}
	if !until.IsZero() {
		data["Until"] = until
	}
	render(w, r, "admin_open.html", data)
}

func (a *App) adminOpen(w http.ResponseWriter, r *http.Request) {
	a.renderOpen(w, r)
}

// adminOpenOn opens the portal. Waiting guests are not approved: each one must
// first accept the activity-recording notice (their page reloads to show it).
func (a *App) adminOpenOn(w http.ResponseWriter, r *http.Request) {
	duration, ok := formDuration(r)
	if !ok {
		http.Error(w, "duration must be HH:MM:SS", http.StatusBadRequest)
		return
	}
	// "close in": a custom number of minutes wins over the preset; none = manual close
	closeMin, _ := strconv.Atoi(r.FormValue("close_min"))
	if closeMin == 0 {
		closeMin, _ = strconv.Atoi(r.FormValue("close_in"))
	}
	var until time.Time
	if closeMin > 0 {
		if closeMin > 7*24*60 {
			http.Error(w, "close_min must be at most 10080 (7 days)", http.StatusBadRequest)
			return
		}
		until = time.Now().Add(time.Duration(closeMin) * time.Minute)
	}
	if err := setOpen(a.db, duration, until); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderOpen(w, r)
}

func (a *App) adminOpenOff(w http.ResponseWriter, r *http.Request) {
	if err := clearOpen(a.db); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderOpen(w, r)
}

// fetchLease runs off the request path: the router can be slow or down, and
// the guest's page must not wait on it.
func (a *App) fetchLease(id int, router, mac string) {
	if l, err := a.router(router).LeaseInfo(mac); err == nil && (l.Hostname != "" || l.Vendor != "") {
		setLease(a.db, id, l)
	}
}

// activeSessions lists approved sessions and marks whether each device is
// still on the network. One router query per router, in parallel and bounded
// to 3s so a dead router can't stall the dashboard poll; a router that
// doesn't answer leaves its devices "unknown", never wrongly "offline".
func (a *App) activeSessions() ([]ApprovedSession, error) {
	sessions, err := listApproved(a.db)
	if err != nil || len(sessions) == 0 {
		return sessions, err
	}
	type res struct {
		router string
		macs   map[string]bool
	}
	names := map[string]bool{}
	for _, s := range sessions {
		names[s.Router] = true
	}
	ch := make(chan res, len(names))
	for name := range names {
		go func(name string) {
			macs, err := a.router(name).OnlineMACs()
			if err != nil {
				macs = nil
			}
			ch <- res{name, macs}
		}(name)
	}
	sets := map[string]map[string]bool{}
	deadline := time.After(3 * time.Second)
	for range names {
		select {
		case r := <-ch:
			sets[r.router] = r.macs
		case <-deadline:
		}
	}
	markOnline(sessions, sets)
	return sessions, nil
}

// markOnline sets each session's State from its router's set of present MACs
// (nil/missing set = router unreachable = unknown).
func markOnline(sessions []ApprovedSession, sets map[string]map[string]bool) {
	for i := range sessions {
		set := sets[sessions[i].Router]
		switch {
		case set == nil:
			sessions[i].State = "unknown"
		case set[strings.ToUpper(sessions[i].MACAddress)]:
			sessions[i].State = "online"
		default:
			sessions[i].State = "offline"
		}
	}
}
