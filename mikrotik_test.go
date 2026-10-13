package main

import "testing"

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
