package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
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
	LeaseInfo(mac string) (Lease, error)
	Connections() ([]Conn, error)
	DNSNames() (map[string]string, error)
	OnlineMACs() (map[string]bool, error)
	ClearVideoHosts() error
	OpenAP() (bool, error)
	SetOpenAP(on bool) error
}

// Conn is one entry of the router's connection-tracking table.
type Conn struct {
	Proto, SrcIP, DstIP string
	SrcPort, DstPort    int
	Up, Down            int64 // bytes originator -> peer, peer -> originator
}

// Lease is what the router's DHCP server knows about a device.
type Lease struct {
	Hostname string // DHCP option 12, e.g. "Daniels-iPhone"
	Vendor   string // DHCP option 60 (class id), e.g. "android-dhcp-13", "MSFT 5.0"
}

type routerOSClient struct {
	address string
	user    string
	pass    string
}

// loadRouters parses ROUTERS, e.g.
//
//	"aphrodite|102.18.61.39:8728|backend-api|pass1;cythera|192.168.1.201:8728|backend-api|pass2"
//
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

// LeaseInfo reads the device's DHCP lease. No lease is not an error: the
// zero Lease comes back.
func (c *routerOSClient) LeaseInfo(mac string) (Lease, error) {
	client, err := c.dial()
	if err != nil {
		return Lease{}, err
	}
	defer client.Close()

	reply, err := client.Run("/ip/dhcp-server/lease/print", "?mac-address="+mac)
	if err != nil || len(reply.Re) == 0 {
		return Lease{}, err
	}
	m := reply.Re[0].Map
	l := Lease{Hostname: m["host-name"], Vendor: m["class-id"]}
	if l.Hostname == "" {
		l.Hostname = m["active-host-name"]
	}
	if l.Vendor == "" {
		l.Vendor = m["active-class-id"]
	}
	return l, nil
}

// parseAddr splits RouterOS's "ip:port" (port absent for icmp).
func parseAddr(s string) (ip string, port int) {
	host, p, err := net.SplitHostPort(s)
	if err != nil {
		return s, 0
	}
	port, _ = strconv.Atoi(p)
	return host, port
}

// Connections returns the router's whole connection table.
func (c *routerOSClient) Connections() ([]Conn, error) {
	client, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	reply, err := client.Run("/ip/firewall/connection/print",
		"=.proplist=protocol,src-address,dst-address,orig-bytes,repl-bytes")
	if err != nil {
		return nil, err
	}
	out := make([]Conn, 0, len(reply.Re))
	for _, re := range reply.Re {
		m := re.Map
		cn := Conn{Proto: m["protocol"]}
		cn.SrcIP, cn.SrcPort = parseAddr(m["src-address"])
		cn.DstIP, cn.DstPort = parseAddr(m["dst-address"])
		cn.Up, _ = strconv.ParseInt(m["orig-bytes"], 10, 64)
		cn.Down, _ = strconv.ParseInt(m["repl-bytes"], 10, 64)
		out = append(out, cn)
	}
	return out, nil
}

// DNSNames maps IPv4 address -> name from the router's DNS cache, to put
// domain names on connections (best effort: the cache is shared by all clients).
func (c *routerOSClient) DNSNames() (map[string]string, error) {
	client, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	reply, err := client.Run("/ip/dns/cache/print", "=.proplist=name,type,data")
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, re := range reply.Re {
		if re.Map["type"] == "A" && re.Map["data"] != "" {
			names[re.Map["data"]] = re.Map["name"]
		}
	}
	return names, nil
}

// OnlineMACs returns the MACs the router currently sees on the guest network
// (ARP table plus hotspot host table, upper-cased). A device that left or
// went to sleep long enough drops out of both.
func (c *routerOSClient) OnlineMACs() (map[string]bool, error) {
	client, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	macs := map[string]bool{}
	for _, cmd := range []string{"/ip/arp/print", "/ip/hotspot/host/print"} {
		reply, err := client.Run(cmd, "=.proplist=mac-address")
		if err != nil {
			return nil, err
		}
		for _, re := range reply.Re {
			if m := re.Map["mac-address"]; m != "" {
				macs[strings.ToUpper(m)] = true
			}
		}
	}
	return macs, nil
}

// wgComment tags the walled-garden entries an earlier version of this app
// added (YouTube hosts). They never worked for HTTPS and caused redirect loops,
// so they are only ever removed now - see ClearVideoHosts.
const wgComment = "aphrodite-service-off"

// ClearVideoHosts removes the entries tagged wgComment and nothing else.
func (c *routerOSClient) ClearVideoHosts() error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer client.Close()

	reply, err := client.Run("/ip/hotspot/walled-garden/print", "?comment="+wgComment)
	if err != nil {
		return err
	}
	for _, re := range reply.Re {
		if _, err := client.Run("/ip/hotspot/walled-garden/remove", "=.id="+re.Map[".id"]); err != nil {
			return err
		}
	}
	return nil
}

// openAPName is the virtual AP created by scripts/install-open-ap.sh.
const openAPName = "wlan1-open"

// OpenAP reports whether the open AP is enabled (error if it isn't installed).
func (c *routerOSClient) OpenAP() (bool, error) {
	client, err := c.dial()
	if err != nil {
		return false, err
	}
	defer client.Close()

	reply, err := client.Run("/interface/wireless/print", "?name="+openAPName, "=.proplist=disabled")
	if err != nil {
		return false, err
	}
	if len(reply.Re) == 0 {
		return false, fmt.Errorf("%s not installed - run scripts/install-open-ap.sh", openAPName)
	}
	return reply.Re[0].Map["disabled"] != "true", nil
}

func (c *routerOSClient) SetOpenAP(on bool) error {
	client, err := c.dial()
	if err != nil {
		return err
	}
	defer client.Close()

	cmd := "/interface/wireless/disable"
	if on {
		cmd = "/interface/wireless/enable"
	}
	_, err = client.Run(cmd, "=numbers="+openAPName)
	return err
}
