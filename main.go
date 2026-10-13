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
	app := &App{db: db, routers: routers, defaultRouter: defaultRouter}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("GET /welcome", app.welcome)
	mux.HandleFunc("GET /welcome/status/{id}", app.welcomeStatus)
	mux.HandleFunc("GET /admin", app.adminPage)
	mux.HandleFunc("GET /admin/pending", app.adminPending)
	mux.HandleFunc("GET /admin/active", app.adminActive)
	mux.HandleFunc("POST /admin/approve/{id}", app.approve)
	mux.HandleFunc("POST /admin/disconnect/{id}", app.adminDisconnect)

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
