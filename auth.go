package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"time"
)

const sessionCookie = "session"
const sessionTTL = 7 * 24 * time.Hour

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// requireAuth gates the admin dashboard - guest-facing routes (/welcome,
// /static) stay public, everything under /admin needs a valid session.
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		user, isAdmin, ok := getAdminSession(a.db, c.Value)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, authUser{user, isAdmin})))
	}
}

type userKey struct{}
type authUser struct {
	Name    string
	IsAdmin bool
}

func currentUser(r *http.Request) authUser {
	u, _ := r.Context().Value(userKey{}).(authUser)
	return u
}

// requireAdmin gates operator management and login history; operators can
// only handle guest sessions.
func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	user := r.FormValue("username")
	pass := r.FormValue("password")

	wantUser := os.Getenv("ADMIN_USER")
	wantPass := os.Getenv("ADMIN_PASS")
	isAdmin := wantUser != "" &&
		subtle.ConstantTimeCompare([]byte(user), []byte(wantUser)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(wantPass)) == 1
	ok := isAdmin
	if !ok && user != wantUser {
		ok = checkUserPassword(a.db, user, pass)
	}
	recordLogin(a.db, user, ok, clientIP(r), r.UserAgent())
	if !ok {
		tmpl.ExecuteTemplate(w, "login.html", map[string]any{"Error": "Invalid username or password"})
		return
	}

	token, err := newSessionToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := createAdminSession(a.db, token, sessionTTL, user, isAdmin); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		deleteAdminSession(a.db, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}
