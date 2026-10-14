package main

import (
	"os"
	"strings"
	"time"

	"github.com/go-routeros/routeros/v3"
)

// dialTimeout bounds how long a stale/unreachable router (e.g. after the
// home ISP's dynamic IP rotates) hangs a request - without this, a TCP
// connect to a dead address can block for minutes with no clear error.
const dialTimeout = 8 * time.Second

// MikroTikClient talks to RouterOS's binary API (ports 8728/8729). Behind an
// interface so handlers can be tested against a fake without real hardware.
//
// RouterOS 6.x has no REST API (that's a v7-only feature) - this router runs
// 6.49.20, so the binary API is the only option here.
//
// Approval uses /ip/hotspot/ip-binding type=bypassed rather than
// /ip/hotspot/user: a device sitting on our walled-garden-permitted welcome
// page never generates a new intercepted request, so login-by=mac never
// gets a chance to fire and the device just sits there with no session.
// A bypass binding takes effect immediately, no new request needed. Time
// enforcement moves to our own DB (approved_at + duration), swept on
// /admin/active's poll - see sweepExpiredSessions in db.go.
type MikroTikClient interface {
	AuthorizeDevice(mac string) error
	DeauthorizeDevice(mac string) error
}

type routerOSClient struct {
	address string
	user    string
	pass    string
}

// loadRouters parses ROUTERS, e.g.
//   "aphrodite|102.18.61.39:8728|backend-api|pass1;cythera|192.168.1.201:8728|backend-api|pass2"
// into a name -> client map. The first entry is the default router, used
// when a guest's login page doesn't specify one (older login.html copies).
func loadRouters() (clients map[string]MikroTikClient, defaultName string) {
	clients = map[string]MikroTikClient{}
	for _, entry := range strings.Split(os.Getenv("ROUTERS"), ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, "|")
		if len(parts) != 4 {
			continue
		}
		name := parts[0]
		clients[name] = &routerOSClient{address: parts[1], user: parts[2], pass: parts[3]}
		if defaultName == "" {
			defaultName = name
		}
	}
	return clients, defaultName
}

// dial opens a fresh connection per call. Traffic volume here (an admin
// approving guests) is far too low to justify a pooled/persistent client.
func (c *routerOSClient) dial() (*routeros.Client, error) {
	return routeros.DialTimeout(c.address, c.user, c.pass, dialTimeout)
}

// AuthorizeDevice grants a device internet access immediately, scoped to our
// own GuestPortal server - this router also runs a separate, pre-existing
// "Aphrodite Temple" hotspot that must stay untouched.
func (c *routerOSClient) AuthorizeDevice(mac string) error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.Run("/ip/hotspot/ip-binding/add",
		"=mac-address="+mac,
		"=type=bypassed",
		"=server=GuestPortal",
	)
	return err
}

func (c *routerOSClient) DeauthorizeDevice(mac string) error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer client.Close()

	reply, err := client.Run("/ip/hotspot/ip-binding/print",
		"?mac-address="+mac,
		"?server=GuestPortal",
	)
	if err != nil {
		return err
	}
	for _, re := range reply.Re {
		if _, err := client.Run("/ip/hotspot/ip-binding/remove", "=.id="+re.Map[".id"]); err != nil {
			return err
		}
	}
	return nil
}
