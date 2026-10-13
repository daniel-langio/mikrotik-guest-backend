package main

import (
	"database/sql"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"strconv"
)

var tmpl = template.Must(template.ParseGlob("templates/*.html"))

func redirectURL() string {
	if u := os.Getenv("REDIRECT_URL"); u != "" {
		return u
	}
	return "https://en.wikipedia.org/wiki/Aphrodite"
}

type App struct {
	db *sql.DB
	mt MikroTikClient
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
	username := "GUEST-" + randSuffix()

	id, err := insertPending(a.db, mac, ip, username)
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
	sessions, err := a.mt.GetActiveSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	tmpl.ExecuteTemplate(w, "admin_active.html", sessions)
}

// approveDevice maps an approved pending session onto the MikroTik call.
// Extracted so it's testable without a live database.
//
// The hotspot profile uses login-by=mac with the default mac-as-username
// mode, which auto-authenticates a client by looking up a /ip hotspot user
// whose *name* equals the device's MAC - not by matching the mac-address
// field. So the RouterOS-side username has to be the MAC itself; sess.Username
// (GUEST-xxxx) stays a purely cosmetic label in our own DB/UI.
func approveDevice(mt MikroTikClient, sess PendingSession, duration string) error {
	return mt.AddHotspotUser(sess.MACAddress, sess.MACAddress, duration)
}

func (a *App) approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	duration := r.FormValue("duration")

	sess, err := getSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := approveDevice(a.mt, sess, duration); err != nil {
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
