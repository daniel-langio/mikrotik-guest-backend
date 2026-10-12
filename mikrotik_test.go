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

func TestApproveDevice_CallsMikroTikWithSessionFields(t *testing.T) {
	fake := &fakeMikroTik{}
	sess := PendingSession{Username: "GUEST-49FX", MACAddress: "AA:BB:CC:DD:EE:FF"}

	if err := approveDevice(fake, sess, "02:00:00"); err != nil {
		t.Fatalf("approveDevice returned error: %v", err)
	}
	if fake.gotName != sess.Username || fake.gotMAC != sess.MACAddress || fake.gotDuration != "02:00:00" {
		t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)",
			fake.gotName, fake.gotMAC, fake.gotDuration,
			sess.Username, sess.MACAddress, "02:00:00")
	}
}
