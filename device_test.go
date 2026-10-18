package main

import "testing"

func TestDeviceOS(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Linux; Android 13; SM-A515F) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36": "Android 13 · SM-A515F",
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36":                            "Android 10",
		"Dalvik/2.1.0 (Linux; U; Android 12; moto g52j 5G Build/S1SYS32.40)":                                                 "Android 12 · moto g52j 5G",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1":             "iOS 17.2",
		"Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148":                                   "iPadOS 16.6",
		"CaptiveNetworkSupport-355.60.2 wispr":                                                                               "Apple (iOS/macOS)",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36":                            "Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15":                               "macOS",
		"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0":                                             "Linux",
		"curl/8.0": "",
		"":         "",
	} {
		if got := deviceOS(ua); got != want {
			t.Errorf("deviceOS(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestOSFromLease(t *testing.T) {
	for l, want := range map[Lease]string{
		{Vendor: "android-dhcp-13"}:               "Android 13",
		{Vendor: "MSFT 5.0"}:                      "Windows",
		{Hostname: "Daniels-iPhone"}:              "iOS",
		{Hostname: "iPad-de-Sarah"}:               "iPadOS",
		{Hostname: "Toms-MacBook-Pro"}:            "macOS",
		{Vendor: "dhcpcd-9.4.1:Linux-6.1:x86_64"}: "Linux",
		{Hostname: "esp32-sensor"}:                "",
	} {
		if got := osFromLease(l); got != want {
			t.Errorf("osFromLease(%+v) = %q, want %q", l, got, want)
		}
	}
}
