package main

import (
	"html"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLegalLineInsteadOfGate(t *testing.T) {
	data := map[string]any{"ID": 7, "MAC": "AA:BB:CC:00:00:01", "Username": "GUEST-X", "Receiver": "+261329977335", "Price": 100, "Mins": 3, "Max": 50000}
	for cookie, wants := range map[string][]string{
		"fr": {"En utilisant notre service, vous acceptez notre ", "politique de confidentialité", "/privacy?s=7"},
		"mg": {"Raha mampiasa ny serivisinay ianao, dia manaiky ny ", "politikan'ny tsiambaratelo", "/privacy?s=7"},
	} {
		r := httptest.NewRequest("GET", "/welcome", nil)
		r.Header.Set("Cookie", "lang="+cookie)
		w := httptest.NewRecorder()
		render(w, r, "welcome.html", data)
		b := html.UnescapeString(w.Body.String())
		for _, want := range wants {
			if !strings.Contains(b, want) {
				t.Errorf("%s: welcome page missing %q", cookie, want)
			}
		}
		for _, gone := range []string{`name="agree"`, "ENREGISTRONS", "MIRAKITRA", "type=\"checkbox\""} {
			if strings.Contains(b, gone) {
				t.Errorf("%s: %q should be gone from the guest page", cookie, gone)
			}
		}
		if !strings.Contains(b, "status/7?g=0") {
			t.Errorf("%s: poller should carry g=0 when the service is on", cookie)
		}
	}
}

func TestServiceDisabledScreen(t *testing.T) {
	data := map[string]any{"ID": 7, "MAC": "AA", "Username": "GUEST-X", "Disabled": true, "Video": "YE7VzlLtp-4",
		"Receiver": "+261329977335", "Price": 100, "Mins": 3, "Max": 50000}
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/welcome", nil), "welcome.html", data)
	b := html.UnescapeString(w.Body.String())
	if h := w.Header().Get("Referrer-Policy"); h != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q (YouTube error 153 without a Referer)", h)
	}
	for _, want := range []string{"Service désactivé par l'admin", "youtube-nocookie.com/embed/YE7VzlLtp-4", "status/7?g=1",
		`referrerpolicy="strict-origin-when-cross-origin"`, "https://www.youtube.com/watch?v=YE7VzlLtp-4", "Regarder sur YouTube"} {
		if !strings.Contains(b, want) {
			t.Errorf("disabled screen missing %q", want)
		}
	}
	if strings.Contains(b, `name="provider"`) || strings.Contains(b, "Approuver maintenant") {
		t.Error("no way to connect while the service is disabled")
	}
}

func TestParseVideoIDs(t *testing.T) {
	text := "https://www.youtube.com/watch?v=YE7VzlLtp-4\nhttps://youtu.be/eRsGyueVLvQ?t=5\n  R6MlUcmOul8  \nnot a video\nhttps://www.youtube.com/embed/TLkA0RELQ1g\n\n"
	got := parseVideoIDs(text)
	want := []string{"YE7VzlLtp-4", "eRsGyueVLvQ", "R6MlUcmOul8", "TLkA0RELQ1g"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, id := range defaultVideos {
		if !ytID.MatchString(id) {
			t.Errorf("default video %q is not a valid id", id)
		}
	}
}

func TestAdminServicePanel(t *testing.T) {
	for disabled, wants := range map[bool][]string{
		false: {"Service actif", "Désactiver le service", "/admin/service/off"},
		true:  {"Service désactivé par l'admin", "Réactiver le service", "/admin/service/on"},
	} {
		w := httptest.NewRecorder()
		render(w, httptest.NewRequest("GET", "/admin/service", nil), "admin_service.html", map[string]any{"Disabled": disabled, "Videos": "https://youtu.be/YE7VzlLtp-4"})
		b := html.UnescapeString(w.Body.String())
		for _, want := range wants {
			if !strings.Contains(b, want) {
				t.Errorf("disabled=%v: panel missing %q", disabled, want)
			}
		}
	}
}

func TestPrivacyPage(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/privacy", nil), "privacy.html",
		map[string]any{"Retention": 90, "Contact": "", "Version": policyVersion, "SessionID": 7, "MAC": "AA:BB"})
	b := w.Body.String()
	for _, want := range []string{"Politique de confidentialité", "Retirer mon consentement", `name="s" value="7"`, policyVersion} {
		if !strings.Contains(b, want) {
			t.Errorf("privacy page missing %q", want)
		}
	}
	w = httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/privacy", nil), "privacy.html", map[string]any{"Retention": 90, "Version": policyVersion})
	if strings.Contains(w.Body.String(), "Retirer mon consentement") {
		t.Error("withdraw button must only show with a valid session context")
	}
}

func TestActivityFor(t *testing.T) {
	conns := []Conn{
		{Proto: "tcp", SrcIP: "10.0.0.5", SrcPort: 51000, DstIP: "142.250.1.1", DstPort: 443, Up: 100, Down: 2000},
		{Proto: "udp", SrcIP: "10.0.0.5", SrcPort: 40000, DstIP: "8.8.8.8", DstPort: 53, Up: 5, Down: 9},
		{Proto: "tcp", SrcIP: "10.0.0.9", SrcPort: 1, DstIP: "1.1.1.1", DstPort: 80},
	}
	got := activityFor(conns, "10.0.0.5", map[string]string{"142.250.1.1": "www.google.com"})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (other device's connection must be excluded)", len(got))
	}
	if got[0].Domain != "www.google.com" || got[0].DstPort != 443 || got[0].Up != 100 || got[0].Down != 2000 {
		t.Errorf("unexpected %+v", got[0])
	}
	if got[0].Key == got[1].Key {
		t.Error("distinct connections must have distinct keys")
	}
}

func TestParseAddrAndBytes(t *testing.T) {
	if ip, port := parseAddr("10.0.0.5:51234"); ip != "10.0.0.5" || port != 51234 {
		t.Errorf("parseAddr = %q %d", ip, port)
	}
	if ip, port := parseAddr("10.0.0.5"); ip != "10.0.0.5" || port != 0 {
		t.Errorf("parseAddr (icmp) = %q %d", ip, port)
	}
	for n, want := range map[int64]string{12: "12 B", 2048: "2.0 KB", 3 << 20: "3.0 MB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestMarkOnline(t *testing.T) {
	sessions := []ApprovedSession{
		{MACAddress: "aa:bb:cc:00:00:01", Router: "r1"}, // present (case-insensitive)
		{MACAddress: "AA:BB:CC:00:00:02", Router: "r1"}, // router answered, device absent
		{MACAddress: "AA:BB:CC:00:00:03", Router: "r2"}, // router didn't answer
	}
	markOnline(sessions, map[string]map[string]bool{"r1": {"AA:BB:CC:00:00:01": true}})
	for i, want := range []string{"online", "offline", "unknown"} {
		if sessions[i].State != want {
			t.Errorf("session %d: %q, want %q", i, sessions[i].State, want)
		}
	}
}

func TestActivityPanelAndActiveDot(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/activity", nil), "admin_activity.html", map[string]any{"Pager": Pager{Base: "/admin/activity", Page: 1}, "Rows": []ActivityRow{{
		LastSeen: time.Now(), Username: "GUEST-X", MAC: "AA", Router: "aphrodite", Proto: "tcp",
		DstIP: "1.2.3.4", Domain: "example.com", DstPort: 443, Up: 1024, Down: 4096}}})
	for _, want := range []string{"GUEST-X", "example.com:443", "1.0 KB", "4.0 KB"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("activity panel missing %q", want)
		}
	}
	w = httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/active", nil), "admin_active.html",
		[]ApprovedSession{{ID: 1, Username: "g", MACAddress: "AA", Router: "r", ExpiresAt: time.Now(), State: "offline"}})
	if !strings.Contains(w.Body.String(), "dot-offline") {
		t.Error("offline session must get the red dot class")
	}
}

func TestPagerAndRows(t *testing.T) {
	rows := make([]int, pageSize+1) // one extra row = there is a next page
	data, p := pageRows(rows, "/admin/history", 2)
	if got := len(data["Rows"].([]int)); got != pageSize {
		t.Errorf("rows = %d, want %d", got, pageSize)
	}
	if !p.HasPrev || !p.HasNext || p.Prev != 1 || p.Next != 3 {
		t.Errorf("pager = %+v", p)
	}
	if _, p := pageRows(make([]int, 3), "/x", 1); p.HasPrev || p.HasNext {
		t.Errorf("short first page must have no prev/next: %+v", p)
	}
	for q, want := range map[string]int{"": 1, "page=0": 1, "page=-4": 1, "page=abc": 1, "page=3": 3} {
		if got := pageParam(httptest.NewRequest("GET", "/?"+q, nil)); got != want {
			t.Errorf("pageParam(%q) = %d, want %d", q, got, want)
		}
	}
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/history", nil), "admin_history.html",
		map[string]any{"Rows": []Login{{Username: "admin", Success: true, At: time.Now()}}, "Pager": Pager{Base: "/admin/history", Page: 2, HasPrev: true, HasNext: true, Prev: 1, Next: 3}})
	for _, want := range []string{"/admin/history?page=1", "/admin/history?page=3", "Page 2", "Précédent", "Suivant"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("history panel missing %q", want)
		}
	}
}

func TestVideoHostsAreWildcardedAndNonEmpty(t *testing.T) {
	if len(videoHosts) == 0 {
		t.Fatal("no hosts to whitelist")
	}
	for _, h := range videoHosts {
		if !strings.HasPrefix(h, "*.") || strings.Count(h, ".") < 2 {
			t.Errorf("unexpected walled-garden host %q", h)
		}
	}
	if wgComment == "" {
		t.Error("entries must be tagged so removal never touches other walled-garden rules")
	}
	w := httptest.NewRecorder()
	render(w, httptest.NewRequest("GET", "/admin/service", nil), "admin_service.html", map[string]any{"Failed": "aphrodite"})
	if !strings.Contains(html.UnescapeString(w.Body.String()), "Routeur injoignable : aphrodite") {
		t.Error("router failure must be shown in the service panel")
	}
}
