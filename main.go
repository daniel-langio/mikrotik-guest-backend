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

	go app.collectLoop()
	mux := app.routes()

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

func (a *App) routes() *http.ServeMux {
	mux := http.NewServeMux()
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always revalidate - this app gets redeployed often, and a browser's
		// default heuristic caching otherwise serves stale CSS/JS silently.
		w.Header().Set("Cache-Control", "no-cache")
		staticFiles.ServeHTTP(w, r)
	}))
	mux.HandleFunc("POST /lang", a.setLang)
	mux.HandleFunc("GET /help", a.help)
	mux.HandleFunc("GET /welcome", a.welcome)
	mux.HandleFunc("GET /welcome/status/{id}", a.welcomeStatus)
	mux.HandleFunc("GET /privacy", a.privacy)
	mux.HandleFunc("POST /consent/withdraw", a.consentWithdraw)
	mux.HandleFunc("GET /admin/activity", a.requireAdmin(a.adminActivity))
	mux.HandleFunc("GET /admin/activity.csv", a.requireAdmin(a.adminActivityCSV))
	mux.HandleFunc("GET /welcome/redirect", a.welcomeRedirect)
	mux.HandleFunc("POST /welcome/pay/{id}", a.pay)
	mux.HandleFunc("GET /welcome/pay/{id}/form", a.payForm)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /admin", a.requireAuth(a.adminPage))
	mux.HandleFunc("GET /admin/pending", a.requireAuth(a.adminPending))
	mux.HandleFunc("GET /admin/active", a.requireAuth(a.adminActive))
	mux.HandleFunc("GET /admin/service", a.requireAdmin(a.adminService))
	mux.HandleFunc("POST /admin/service/off", a.requireAdmin(a.adminServiceSet(true)))
	mux.HandleFunc("POST /admin/service/on", a.requireAdmin(a.adminServiceSet(false)))
	mux.HandleFunc("POST /admin/service/videos", a.requireAdmin(a.adminServiceVideos))
	mux.HandleFunc("GET /admin/open", a.requireAdmin(a.adminOpen))
	mux.HandleFunc("POST /admin/open", a.requireAdmin(a.adminOpenOn))
	mux.HandleFunc("POST /admin/open/off", a.requireAdmin(a.adminOpenOff))
	mux.HandleFunc("GET /admin/devices", a.requireAdmin(a.adminDevices))
	mux.HandleFunc("GET /admin/devices.csv", a.requireAdmin(a.adminDevicesCSV))
	mux.HandleFunc("GET /admin/users", a.requireAdmin(a.adminUsers))
	mux.HandleFunc("POST /admin/users", a.requireAdmin(a.adminCreateUser))
	mux.HandleFunc("POST /admin/users/delete/{username}", a.requireAdmin(a.adminDeleteUser))
	mux.HandleFunc("GET /admin/history", a.requireAdmin(a.adminHistory))
	mux.HandleFunc("POST /admin/pending/delete/{id}", a.requireAuth(a.adminDeletePending))
	mux.HandleFunc("POST /admin/pending/clear", a.requireAuth(a.adminClearPending))
	mux.HandleFunc("POST /admin/approve/{id}", a.requireAuth(a.approve))
	mux.HandleFunc("POST /admin/disconnect/{id}", a.requireAuth(a.adminDisconnect))
	return mux
}
