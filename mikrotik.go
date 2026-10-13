package main

import (
	"os"

	"github.com/go-routeros/routeros/v3"
)

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
