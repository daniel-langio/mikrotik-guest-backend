package main

import (
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-routeros/routeros/v3"
)

// MikroTikClient talks to RouterOS's binary API (ports 8728/8729). Behind an
// interface so handlers can be tested against a fake without real hardware.
//
// RouterOS 6.x has no REST API (that's a v7-only feature) - this router runs
// 6.49.20, so the binary API is the only option here.
type MikroTikClient interface {
	AddHotspotUser(name, mac, limitUptime string) error
	GetActiveSessions() ([]ActiveSession, error)
	// RemoveExpiredUsers deletes GuestPortal hotspot users whose accumulated
	// uptime has reached their limit-uptime (RouterOS disconnects the session
	// itself but never cleans up the stale user record) and returns the MAC
	// addresses removed, so the caller can reconcile its own DB.
	RemoveExpiredUsers() ([]string, error)
}

type ActiveSession struct {
	User    string
	Address string
	MacAddr string
	Uptime  string
}

type routerOSClient struct {
	address string
	user    string
	pass    string
}

func newRouterOSClient() *routerOSClient {
	return &routerOSClient{
		address: os.Getenv("MIKROTIK_HOST"), // host:port, e.g. "102.18.61.39:8728"
		user:    os.Getenv("MIKROTIK_USER"),
		pass:    os.Getenv("MIKROTIK_PASS"),
	}
}

// dial opens a fresh connection per call. Traffic volume here (an admin
// approving guests) is far too low to justify a pooled/persistent client.
func (c *routerOSClient) dial() (*routeros.Client, error) {
	return routeros.Dial(c.address, c.user, c.pass)
}

func (c *routerOSClient) AddHotspotUser(name, mac, limitUptime string) error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer client.Close()

	// Scoped to our own hotspot server/profile - this router also runs a
	// separate, pre-existing "Aphrodite Temple" hotspot that must stay untouched.
	_, err = client.Run("/ip/hotspot/user/add",
		"=name="+name,
		"=mac-address="+mac,
		"=limit-uptime="+limitUptime,
		"=server=GuestPortal",
		"=profile=guest-portal-profile",
	)
	return err
}

func (c *routerOSClient) GetActiveSessions() ([]ActiveSession, error) {
	client, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Scoped to our own hotspot server - this router also runs a separate,
	// pre-existing "Aphrodite Temple" hotspot that must never show up here.
	reply, err := client.Run("/ip/hotspot/active/print", "?server=GuestPortal")
	if err != nil {
		return nil, err
	}

	sessions := make([]ActiveSession, 0, len(reply.Re))
	for _, re := range reply.Re {
		sessions = append(sessions, ActiveSession{
			User:    re.Map["user"],
			Address: re.Map["address"],
			MacAddr: re.Map["mac-address"],
			Uptime:  re.Map["uptime"],
		})
	}
	return sessions, nil
}

var routerOSDurationPart = regexp.MustCompile(`(\d+)([wdhms])`)

// parseRouterOSDuration parses RouterOS's compact duration format, e.g.
// "4w1d23h22m50s" or "0s". Unknown/empty input parses as zero.
func parseRouterOSDuration(s string) time.Duration {
	var total time.Duration
	for _, m := range routerOSDurationPart.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "w":
			total += time.Duration(n) * 7 * 24 * time.Hour
		case "d":
			total += time.Duration(n) * 24 * time.Hour
		case "h":
			total += time.Duration(n) * time.Hour
		case "m":
			total += time.Duration(n) * time.Minute
		case "s":
			total += time.Duration(n) * time.Second
		}
	}
	return total
}

func (c *routerOSClient) RemoveExpiredUsers() ([]string, error) {
	client, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	reply, err := client.Run("/ip/hotspot/user/print", "?server=GuestPortal")
	if err != nil {
		return nil, err
	}

	var removedMACs []string
	for _, re := range reply.Re {
		limit := re.Map["limit-uptime"]
		if limit == "" {
			continue // no limit set, never expires
		}
		if parseRouterOSDuration(re.Map["uptime"]) < parseRouterOSDuration(limit) {
			continue
		}
		if _, err := client.Run("/ip/hotspot/user/remove", "=.id="+re.Map[".id"]); err != nil {
			continue
		}
		removedMACs = append(removedMACs, re.Map["mac-address"])
	}
	return removedMACs, nil
}
