package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Operators are extra logins (stored here) that can handle guest sessions but
// not manage users or read login history. The admin stays env-configured.

const pbkdf2Iter = 600000

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(h), err
}

func verifyPassword(stored, pw string) bool {
	saltHex, hashHex, ok := strings.Cut(stored, "$")
	if !ok {
		return false
	}
	salt, err1 := hex.DecodeString(saltHex)
	want, err2 := hex.DecodeString(hashHex)
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func checkUserPassword(db *sql.DB, username, pw string) bool {
	var stored string
	if db.QueryRow(`SELECT password_hash FROM users WHERE username = $1`, username).Scan(&stored) != nil {
		return false
	}
	return verifyPassword(stored, pw)
}

type User struct {
	Username  string
	CreatedAt time.Time
}

func listUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query(`SELECT username, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type Login struct {
	Username  string
	Success   bool
	IP        string
	UserAgent string
	At        time.Time
}

func recordLogin(db *sql.DB, username string, success bool, ip, ua string) {
	db.Exec(`INSERT INTO login_history (username, success, ip, user_agent) VALUES ($1, $2, $3, $4)`,
		username, success, ip, ua)
}

func listLogins(db *sql.DB, limit, offset int) ([]Login, error) {
	rows, err := db.Query(`SELECT username, success, COALESCE(ip, ''), COALESCE(user_agent, ''), at
		FROM login_history ORDER BY at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Login
	for rows.Next() {
		var l Login
		if err := rows.Scan(&l.Username, &l.Success, &l.IP, &l.UserAgent, &l.At); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// clientIP prefers X-Forwarded-For (Cloud Run sits behind a proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var usernameFormat = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

func (a *App) renderUsers(w http.ResponseWriter, r *http.Request, errKey string) {
	users, err := listUsers(a.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, r, "admin_users.html", map[string]any{"Users": users, "Error": errKey})
}

func (a *App) adminUsers(w http.ResponseWriter, r *http.Request) {
	a.renderUsers(w, r, "")
}

func (a *App) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	name, pw := strings.ToLower(strings.TrimSpace(r.FormValue("username"))), r.FormValue("password")
	switch {
	case !usernameFormat.MatchString(name):
		a.renderUsers(w, r, "err.username")
		return
	case name == strings.ToLower(os.Getenv("ADMIN_USER")):
		a.renderUsers(w, r, "err.reserved")
		return
	case len(pw) < 8:
		a.renderUsers(w, r, "err.pwlen")
		return
	}
	hash, err := hashPassword(pw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := a.db.Exec(`INSERT INTO users (username, password_hash) VALUES ($1, $2)`, name, hash); err != nil {
		if isUniqueViolation(err) {
			a.renderUsers(w, r, "err.userexists")
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderUsers(w, r, "")
}

func (a *App) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("username")
	a.db.Exec(`DELETE FROM admin_sessions WHERE username = $1 AND NOT is_admin`, name)
	if _, err := a.db.Exec(`DELETE FROM users WHERE username = $1`, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderUsers(w, r, "")
}

func (a *App) adminHistory(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	logins, err := listLogins(a.db, pageSize+1, (page-1)*pageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, _ := pageRows(logins, "/admin/history", page)
	render(w, r, "admin_history.html", data)
}

func (a *App) adminDeletePending(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := deletePending(a.db, id, "admin:"+currentUser(r).Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.adminPending(w, r)
}

func (a *App) adminClearPending(w http.ResponseWriter, r *http.Request) {
	if err := clearPending(a.db, "admin:"+currentUser(r).Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.adminPending(w, r)
}
