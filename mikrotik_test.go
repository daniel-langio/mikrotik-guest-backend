package main

import (
	"testing"
	"time"
)

func TestParseRouterOSDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"":              0,
		"0s":            0,
		"1h30m0s":       90 * time.Minute,
		"2h":            2 * time.Hour,
		"4w1d23h22m50s": 4*7*24*time.Hour + 24*time.Hour + 23*time.Hour + 22*time.Minute + 50*time.Second,
	}
	for in, want := range cases {
		if got := parseRouterOSDuration(in); got != want {
			t.Errorf("parseRouterOSDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

type fakeMikroTik struct {
	gotName     string
	gotMAC      string
	gotDuration string
}

func (f *fakeMikroTik) AddHotspotUser(name, mac, limitUptime string) error {
	f.gotName, f.gotMAC, f.gotDuration = name, mac, limitUptime
	return nil
}

func (f *fakeMikroTik) GetActiveSessions() ([]ActiveSession, error) { return nil, nil }

func (f *fakeMikroTik) RemoveExpiredUsers() ([]string, error) { return nil, nil }

func TestApproveDevice_UsesMACAsRouterOSUsername(t *testing.T) {
	fake := &fakeMikroTik{}
	sess := PendingSession{Username: "GUEST-49FX", MACAddress: "AA:BB:CC:DD:EE:FF"}

	if err := approveDevice(fake, sess, "02:00:00"); err != nil {
		t.Fatalf("approveDevice returned error: %v", err)
	}
	// RouterOS's login-by=mac (mac-as-username mode) matches on the hotspot
	// user's *name*, not its mac-address field - so the name must be the MAC.
	if fake.gotName != sess.MACAddress || fake.gotMAC != sess.MACAddress || fake.gotDuration != "02:00:00" {
		t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)",
			fake.gotName, fake.gotMAC, fake.gotDuration,
			sess.MACAddress, sess.MACAddress, "02:00:00")
	}
}
