package main

import (
	"database/sql"
	"html/template"
	"math/rand"
	"net/http"
	"strconv"
)

var tmpl = template.Must(template.ParseGlob("templates/*.html"))

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

	if _, err := insertPending(a.db, mac, ip, username); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.ExecuteTemplate(w, "welcome.html", map[string]any{"Username": username})
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
func approveDevice(mt MikroTikClient, sess PendingSession, duration string) error {
	return mt.AddHotspotUser(sess.Username, sess.MACAddress, duration)
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
