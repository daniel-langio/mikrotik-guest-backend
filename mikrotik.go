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
type MikroTikClient interface {
	AddHotspotUser(name, mac, limitUptime string) error
	GetActiveSessions() ([]ActiveSession, error)
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
