package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDurationFor(t *testing.T) {
	for amount, want := range map[int]string{100: "0:03:00", 2000: "1:00:00", 3300: "1:39:00", 50000: "25:00:00"} {
		got, ok := durationFor(amount)
		if !ok || got != want || !durationFormat.MatchString(got) {
			t.Errorf("durationFor(%d) = %q, %v; want %q", amount, got, ok, want)
		}
	}
	for _, bad := range []int{0, 50, 150, -100, 50100} {
		if _, ok := durationFor(bad); ok {
			t.Errorf("durationFor(%d) accepted", bad)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{"034 12 345 67": "+261341234567", "261341234567": "+261341234567", "+261341234567": "+261341234567"} {
		if got, ok := normalizePhone(in); !ok || got != want {
			t.Errorf("normalizePhone(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := normalizePhone("12345"); ok {
		t.Error("accepted short number")
	}
}

func TestIfayClaimAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/payments/claims" {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["receiverPhone"] != "+261340000000" || b["pspRef"] != "R1" || b["type"] != "MVOLA" || b["amount"] != float64(100) {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"id":"abc","status":"PENDING","amount":null}`))
			return
		}
		w.Write([]byte(`{"id":"abc","status":"VERIFIED","amount":100}`))
	}))
	defer srv.Close()
	c := &Ifay{srv.URL, "k", "+261340000000", srv.Client()}
	p, err := c.Claim("+261341234567", 100, "MVOLA", "R1")
	if err != nil || p.ID != "abc" || p.Status != "PENDING" {
		t.Fatalf("Claim = %+v, %v", p, err)
	}
	if st, err := c.Status("abc"); err != nil || st != "VERIFIED" {
		t.Fatalf("Status = %q, %v", st, err)
	}
	bad := &Ifay{srv.URL, "wrong", "+261340000000", srv.Client()}
	if _, err := bad.Status("abc"); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("correct horse")
	if err != nil || !verifyPassword(h, "correct horse") || verifyPassword(h, "wrong") || verifyPassword("junk", "x") {
		t.Fatal("password hash roundtrip failed")
	}
}
