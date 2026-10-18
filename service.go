package main

import (
	"database/sql"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Kill switch: while "service_disabled" is set, new guests can't connect (no
// payment, no open-portal grant) and see a message plus a random YouTube
// video instead. Sessions that are already approved keep running.

// Official Blender open movies: long-lived, embeddable, free to show.
var defaultVideos = []string{"YE7VzlLtp-4", "eRsGyueVLvQ", "R6MlUcmOul8", "TLkA0RELQ1g", "WhWc3b3KhnY", "Y-rmzh0PI3c"}

var ytID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var ytURL = regexp.MustCompile(`(?:v=|youtu\.be/|embed/|shorts/|live/)([A-Za-z0-9_-]{11})`)

// parseVideoIDs reads one YouTube URL or bare id per line; junk lines are skipped.
func parseVideoIDs(text string) []string {
	var ids []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if ytID.MatchString(line) {
			ids = append(ids, line)
		} else if m := ytURL.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func getSetting(db *sql.DB, key string) string {
	var v string
	db.QueryRow(`SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	return v
}

func putSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

func serviceDisabled(db *sql.DB) bool {
	return getSetting(db, "service_disabled") == "1"
}

// pickVideo returns a random configured video, or a random default.
func pickVideo(db *sql.DB) string {
	ids := parseVideoIDs(getSetting(db, "disabled_videos"))
	if len(ids) == 0 {
		ids = defaultVideos
	}
	return ids[rand.Intn(len(ids))]
}

func (a *App) renderService(w http.ResponseWriter, r *http.Request, failed ...string) {
	render(w, r, "admin_service.html", map[string]any{
		"Disabled": serviceDisabled(a.db),
		"Videos":   getSetting(a.db, "disabled_videos"),
		"Failed":   strings.Join(failed, ", "),
	})
}

// syncVideoHosts makes every router's walled garden match the switch: YouTube
// whitelisted while the service is off, removed while it is on. Routers are
// contacted in parallel; the names of the ones that failed come back.
func (a *App) syncVideoHosts() (failed []string) {
	disabled := serviceDisabled(a.db)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, mt := range a.routers {
		wg.Add(1)
		go func(name string, mt MikroTikClient) {
			defer wg.Done()
			if err := mt.AllowVideoHosts(disabled); err != nil {
				log.Printf("walled garden %s: %v", name, err)
				mu.Lock()
				failed = append(failed, name)
				mu.Unlock()
			}
		}(name, mt)
	}
	wg.Wait()
	return failed
}

func (a *App) adminService(w http.ResponseWriter, r *http.Request) { a.renderService(w, r) }

func (a *App) adminServiceSet(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.PostForm == nil {
			r.ParseForm()
		}
		if v, ok := r.PostForm["videos"]; ok {
			putSetting(a.db, "disabled_videos", strings.TrimSpace(v[0]))
		}
		val := ""
		if disabled {
			val = "1"
		}
		if err := putSetting(a.db, "service_disabled", val); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderService(w, r, a.syncVideoHosts()...)
	}
}

// adminServiceVideos saves the video list without touching the switch.
func (a *App) adminServiceVideos(w http.ResponseWriter, r *http.Request) {
	if err := putSetting(a.db, "disabled_videos", strings.TrimSpace(r.FormValue("videos"))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderService(w, r)
}
