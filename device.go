package main

import (
	"regexp"
	"strings"
)

var (
	androidRe = regexp.MustCompile(`Android ([\d.]+)(?:; ([^;)]+))?`)
	iosRe     = regexp.MustCompile(`(iPhone|iPad|iPod).*?OS (\d+)[_.](\d+)`)
)

// deviceOS names the guest's system from the portal request's User-Agent.
// Best effort: captive-portal mini browsers and "desktop site" modes report
// less (iOS' captive sheet sends no OS at all), and a device can spoof it.
func deviceOS(ua string) string {
	switch {
	case strings.Contains(ua, "CaptiveNetworkSupport"):
		return "Apple (iOS/macOS)"
	case androidRe.MatchString(ua):
		m := androidRe.FindStringSubmatch(ua)
		out := "Android " + m[1]
		model := strings.TrimSpace(strings.SplitN(m[2], " Build/", 2)[0])
		if len(model) > 1 { // recent Chrome freezes the model to "K"
			out += " · " + model
		}
		return out
	case iosRe.MatchString(ua):
		m := iosRe.FindStringSubmatch(ua)
		name := "iOS"
		if m[1] == "iPad" {
			name = "iPadOS"
		}
		return name + " " + m[2] + "." + m[3]
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	}
	return ""
}
