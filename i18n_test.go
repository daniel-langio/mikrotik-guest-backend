package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
