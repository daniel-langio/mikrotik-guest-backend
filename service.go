package main

import (
	"database/sql"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

// Kill switch: while "service_disabled" is set, new guests can't connect (no
// payment, no open-portal grant) and see a message plus a random YouTube
// video instead. Sessions that are already approved keep running.

// Videos shown while the service is switched off: the admin's own list if set,
// otherwise these. YouTube's HTTPS hosts must be reachable before login for
// them to play (see the router's walled garden); guests whose connection blocks
// them can tap "video doesn't show?" to get a bundled clip from this portal's
// own domain (static/fallback, Blender Foundation open movies, CC BY 4.0).
var defaultVideos = []string{"RHQMl7SLp8s", "_H9ucMJfmpU", "Wg_jwgmwt-U", "OtunhUigXBE"}

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

// fallbackClips lists the bundled clips as URL paths.
func fallbackClips() []string {
	files, _ := filepath.Glob("static/fallback/*.mp4")
	for i, f := range files {
		files[i] = "/" + filepath.ToSlash(f)
	}
	return files
}

// pickVideo returns a random video id from the admin's list, else the defaults.
func pickVideo(db *sql.DB) string {
	ids := parseVideoIDs(getSetting(db, "disabled_videos"))
	if len(ids) == 0 {
		ids = defaultVideos
	}
	return ids[rand.Intn(len(ids))]
}

// welcomeClip swaps the YouTube embed for a bundled clip, for guests whose
// connection doesn't let YouTube through.
func (a *App) welcomeClip(w http.ResponseWriter, r *http.Request) {
	clips := fallbackClips()
	if len(clips) == 0 {
		http.NotFound(w, r)
		return
	}
	render(w, r, "clip.html", map[string]any{"Clip": clips[rand.Intn(len(clips))]})
}

func (a *App) renderService(w http.ResponseWriter, r *http.Request) {
	render(w, r, "admin_service.html", map[string]any{
		"Disabled": serviceDisabled(a.db),
		"Videos":   getSetting(a.db, "disabled_videos"),
	})
}

// clearWalledGarden removes the YouTube walled-garden entries an earlier
// version added to the routers (idempotent; remove this once routers are clean).
func (a *App) clearWalledGarden() {
	for name, mt := range a.routers {
		go func(name string, mt MikroTikClient) {
			if err := mt.ClearVideoHosts(); err != nil {
				log.Printf("walled garden cleanup %s: %v", name, err)
			}
		}(name, mt)
	}
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
		a.renderService(w, r)
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
