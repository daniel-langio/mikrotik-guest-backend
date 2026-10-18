package main

import "testing"

type fakeMikroTik struct {
	authorizedMAC   string
	deauthorizedMAC string
}

func (f *fakeMikroTik) AuthorizeDevice(mac string) error {
	f.authorizedMAC = mac
	return nil
}

func (f *fakeMikroTik) DeauthorizeDevice(mac string) error {
	f.deauthorizedMAC = mac
	return nil
}

func (f *fakeMikroTik) LeaseInfo(mac string) (Lease, error) {
	return Lease{Hostname: "Demo-Phone", Vendor: "android-dhcp-13"}, nil
}

func (f *fakeMikroTik) Connections() ([]Conn, error) { return nil, nil }

func (f *fakeMikroTik) DNSNames() (map[string]string, error) { return nil, nil }

func (f *fakeMikroTik) OnlineMACs() (map[string]bool, error) {
	return map[string]bool{"AA:BB:CC:DD:EE:FF": true}, nil
}

func TestFakeMikroTik_RecordsCalls(t *testing.T) {
	fake := &fakeMikroTik{}
	if err := fake.AuthorizeDevice("AA:BB:CC:DD:EE:FF"); err != nil {
		t.Fatalf("AuthorizeDevice returned error: %v", err)
	}
	if fake.authorizedMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("authorizedMAC = %q, want AA:BB:CC:DD:EE:FF", fake.authorizedMAC)
	}
	if err := fake.DeauthorizeDevice("AA:BB:CC:DD:EE:FF"); err != nil {
		t.Fatalf("DeauthorizeDevice returned error: %v", err)
	}
	if fake.deauthorizedMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("deauthorizedMAC = %q, want AA:BB:CC:DD:EE:FF", fake.deauthorizedMAC)
	}
}
