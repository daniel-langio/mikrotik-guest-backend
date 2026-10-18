package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Every key used by a template or Go code must exist in both languages, and
// both languages must define the same keys.
func TestI18nKeys(t *testing.T) {
	for k := range dict["fr"] {
		if _, ok := dict["mg"][k]; !ok {
			t.Errorf("mg missing %q", k)
		}
	}
	for k := range dict["mg"] {
		if _, ok := dict["fr"][k]; !ok {
			t.Errorf("fr missing %q", k)
		}
	}
	used := map[string]bool{}
	tplKey := regexp.MustCompile(`\{\{t "([a-z0-9_.]+)"`)
	goKey := regexp.MustCompile(`tr\([^,()]+(?:\([^)]*\))?, "([a-z0-9_.]+)"`)
	files, _ := filepath.Glob("templates/*.html")
	goFiles, _ := filepath.Glob("*.go")
	for _, f := range append(files, goFiles...) {
		if strings.HasSuffix(f, "_test.go") || f == "i18n.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		re := goKey
		if strings.HasSuffix(f, ".html") {
			re = tplKey
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
	}
	// keys passed indirectly (renderUsers error keys, js.* via i18n.html)
	for k := range used {
		if _, ok := dict["fr"][k]; !ok {
			t.Errorf("key %q used but not defined", k)
		}
	}
	if len(used) < 40 {
		t.Errorf("only found %d used keys - regex broken?", len(used))
	}
}

func TestRenderLanguage(t *testing.T) {
	for _, tc := range []struct{ cookie, want string }{
		{"", "Approuver maintenant"},
		{"fr", "Approuver maintenant"},
		{"mg", "Ankatoavo izao"},
		{"xx", "Approuver maintenant"},
	} {
		r := httptest.NewRequest("GET", "/welcome", nil)
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "lang", Value: tc.cookie})
		}
		w := httptest.NewRecorder()
		render(w, r, "welcome.html", map[string]any{"ID": 1, "Username": "GUEST-X", "Receiver": "+261329977335", "Price": 100, "Mins": 3, "Max": 50000})
		body := w.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("cookie %q: %q not in body", tc.cookie, tc.want)
		}
		if strings.Contains(body, `name="provider"`) {
			t.Errorf("cookie %q: pay form shown before the button click", tc.cookie)
		}
	}
	// after the click the form appears
	r := httptest.NewRequest("GET", "/welcome/pay/1/form", nil)
	w := httptest.NewRecorder()
	render(w, r, "pay.html", map[string]any{"ID": 1, "Receiver": "+261329977335", "ShowForm": true, "Price": 100, "Mins": 3, "Max": 50000})
	if !strings.Contains(w.Body.String(), `name="provider"`) {
		t.Error("pay form missing after click")
	}
}

func TestSetLangRedirectsSameOriginOnly(t *testing.T) {
	a := &App{}
	for ref, want := range map[string]string{
		"https://x.example/welcome?mac=AA&ip=1": "/welcome?mac=AA&ip=1",
		"https://x.example//evil.com":           "/",
		"":                                      "/",
	} {
		r := httptest.NewRequest("POST", "/lang", strings.NewReader("lang=mg"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Referer", ref)
		w := httptest.NewRecorder()
		a.setLang(w, r)
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("referer %q -> %q, want %q", ref, got, want)
		}
		if c := w.Result().Cookies(); len(c) != 1 || c[0].Value != "mg" {
			t.Errorf("lang cookie not set: %v", c)
		}
	}
}

func TestHelpPage(t *testing.T) {
	a := &App{}
	for cookie, want := range map[string]string{"": "Comment ça marche", "mg": "Ahoana ny fampiasana"} {
		r := httptest.NewRequest("GET", "/help", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "lang", Value: cookie})
		}
		w := httptest.NewRecorder()
		a.help(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("cookie %q: code %d, %q missing", cookie, w.Code, want)
		}
	}
}

func TestAdminOpenPanel(t *testing.T) {
	for _, tc := range []struct {
		duration, want, notWant string
	}{
		{"", "Ouvrir le portail", "Fermer le portail"},
		{"01:00:00", "Fermer le portail", "Ouvrir le portail"},
	} {
		w := httptest.NewRecorder()
		render(w, httptest.NewRequest("GET", "/admin/open", nil), "admin_open.html", map[string]any{"Duration": tc.duration})
		b := w.Body.String()
		if !strings.Contains(b, tc.want) || strings.Contains(b, tc.notWant) {
			t.Errorf("duration %q: want %q and not %q in %s", tc.duration, tc.want, tc.notWant, b)
		}
	}
}

func TestWelcomeAcceptedHasNoPoller(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/welcome", nil), "welcome.html",
		map[string]any{"ID": 1, "Username": "GUEST-X", "Accepted": true})
	b := w.Body.String()
	if !strings.Contains(b, "Approuvé") || strings.Contains(b, `hx-get="/welcome/status/`) {
		t.Errorf("open-portal welcome should show approval and stop polling: %s", b)
	}
}

func TestFormDuration(t *testing.T) {
	for form, want := range map[string]string{
		"duration=02:00:00":                 "02:00:00",
		"duration=02:00:00&custom_m=45":     "0:45:00",
		"custom_h=1&custom_m=30&custom_s=0": "1:30:00",
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got, ok := formDuration(r); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", form, got, ok, want)
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader("duration=abc"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, ok := formDuration(r); ok {
		t.Error("accepted invalid duration")
	}
}

func TestAdminOpenPanelShowsCloseTime(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/open", nil), "admin_open.html",
		map[string]any{"Duration": "01:00:00", "Until": time.Date(2026, 10, 18, 18, 30, 0, 0, time.UTC)})
	if b := w.Body.String(); !strings.Contains(b, "Ferme à") || !strings.Contains(b, "2026-10-18T18:30:00Z") {
		t.Errorf("close time missing: %s", b)
	}
	w = httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/open", nil), "admin_open.html", map[string]any{"Duration": "01:00:00"})
	if !strings.Contains(w.Body.String(), "fermeture manuelle") {
		t.Error("manual-close note missing")
	}
	w = httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/open", nil), "admin_open.html", map[string]any{})
	if !strings.Contains(w.Body.String(), `name="close_in"`) {
		t.Error("close-in control missing from the open form")
	}
}

func TestDeviceCSVAndPanel(t *testing.T) {
	ev := []DeviceEvent{{At: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), Event: "approved", Actor: "payment", SessionID: 7,
		MAC: "AA:BB:CC:00:00:01", IP: "10.0.0.5", Username: "GUEST-X", Router: "aphrodite", OS: "Android 13 · SM-A515F",
		Hostname: "Demo-Phone", VendorClass: "android-dhcp-13", Provider: "ORANGE_MONEY", Ref: "PP000000.0000.X00000", Amount: 500, Duration: "0:15:00"}}
	var sb strings.Builder
	if err := writeDeviceCSV(&sb, ev); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hostname,vendor_class", "Demo-Phone,android-dhcp-13", "PP000000.0000.X00000,500,0:15:00"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("csv missing %q: %s", want, sb.String())
		}
	}
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/devices", nil), "admin_devices.html", map[string]any{"Rows": ev, "Pager": Pager{Base: "/admin/devices", Page: 1}})
	for _, want := range []string{"GUEST-X", "approuvé", "paiement", "Demo-Phone", "ORANGE_MONEY"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("panel missing %q", want)
		}
	}
}
