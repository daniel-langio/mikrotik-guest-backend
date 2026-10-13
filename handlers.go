package main

import (
	"database/sql"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var tmpl = template.Must(template.ParseGlob("templates/*.html"))

// Matches MikroTik's limit-uptime format (HH:MM:SS, hours uncapped). Enforced
// server-side since a malformed value would break the duration::interval
// cast in sweepExpiredSessions for every approved row, not just this one.
var durationFormat = regexp.MustCompile(`^\d{1,4}:\d{2}:\d{2}$`)

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
		tmpl.ExecuteTemplate(w, "welcome.html", map[string]any{"Username": sess.Username, "ID": sess.ID})
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
	tmpl.ExecuteTemplate(w, "welcome.html", map[string]any{"Username": username, "ID": id})
}

// welcomeStatus is polled by the guest's own welcome page. Once an admin
// approves the session, it tells htmx to redirect the browser (HX-Redirect)
// instead of swapping any content - the guest never has to do anything.
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
	if sess.Status == "approved" {
		w.Header().Set("HX-Redirect", redirectURL())
	}
}

func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "admin.html", nil)
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
	if custom := strings.TrimSpace(r.FormValue("custom_duration")); custom != "" {
		duration = custom
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
