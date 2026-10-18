package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	loadEnvFile(".env")

	db, err := openDB()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	routers, defaultRouter := loadRouters()
	if len(routers) == 0 {
		log.Fatal("ROUTERS is empty - need at least one name|host:port|user|pass entry")
	}
	app := &App{db: db, routers: routers, defaultRouter: defaultRouter, ifay: ifayFromEnv()}

	mux := http.NewServeMux()
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always revalidate - this app gets redeployed often, and a browser's
		// default heuristic caching otherwise serves stale CSS/JS silently.
		w.Header().Set("Cache-Control", "no-cache")
		staticFiles.ServeHTTP(w, r)
	}))
	mux.HandleFunc("POST /lang", app.setLang)
	mux.HandleFunc("GET /welcome", app.welcome)
	mux.HandleFunc("GET /welcome/status/{id}", app.welcomeStatus)
	mux.HandleFunc("GET /welcome/redirect", app.welcomeRedirect)
	mux.HandleFunc("POST /welcome/pay/{id}", app.pay)
	mux.HandleFunc("GET /welcome/pay/{id}/form", app.payForm)
	mux.HandleFunc("GET /login", app.loginPage)
	mux.HandleFunc("POST /login", app.login)
	mux.HandleFunc("POST /logout", app.logout)
	mux.HandleFunc("GET /admin", app.requireAuth(app.adminPage))
	mux.HandleFunc("GET /admin/pending", app.requireAuth(app.adminPending))
	mux.HandleFunc("GET /admin/active", app.requireAuth(app.adminActive))
	mux.HandleFunc("GET /admin/users", app.requireAdmin(app.adminUsers))
	mux.HandleFunc("POST /admin/users", app.requireAdmin(app.adminCreateUser))
	mux.HandleFunc("POST /admin/users/delete/{username}", app.requireAdmin(app.adminDeleteUser))
	mux.HandleFunc("GET /admin/history", app.requireAdmin(app.adminHistory))
	mux.HandleFunc("POST /admin/pending/delete/{id}", app.requireAuth(app.adminDeletePending))
	mux.HandleFunc("POST /admin/pending/clear", app.requireAuth(app.adminClearPending))
	mux.HandleFunc("POST /admin/approve/{id}", app.requireAuth(app.approve))
	mux.HandleFunc("POST /admin/disconnect/{id}", app.requireAuth(app.adminDisconnect))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// loadEnvFile is a minimal .env loader (stdlib only). No-op if the file
// doesn't exist — production sets real env vars instead.
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
