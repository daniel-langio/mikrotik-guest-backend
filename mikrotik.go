package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// MikroTikClient talks to RouterOS's REST API. Behind an interface so
// handlers can be tested against a fake without real hardware.
type MikroTikClient interface {
	AddHotspotUser(name, mac, limitUptime string) error
	GetActiveSessions() ([]ActiveSession, error)
}

type ActiveSession struct {
	User    string `json:"user"`
	Address string `json:"address"`
	MacAddr string `json:"mac-address"`
	Uptime  string `json:"uptime"`
}

type routerOSClient struct {
	baseURL string
	user    string
	pass    string
	http    *http.Client
}

func newRouterOSClient() *routerOSClient {
	return &routerOSClient{
		baseURL: "https://" + os.Getenv("MIKROTIK_HOST"),
		user:    os.Getenv("MIKROTIK_USER"),
		pass:    os.Getenv("MIKROTIK_PASS"),
		http: &http.Client{
			// ponytail: router likely has a self-signed cert; skip verify for
			// now, add a pinned CA if/when it gets a real certificate.
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
	}
}

func (c *routerOSClient) do(method, path string, body, out any) error {
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("mikrotik API %s %s: status %d", method, path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// AddHotspotUser creates the hotspot user record. RouterOS's REST API uses
// PUT on the collection endpoint to add an item (not POST, despite the
// illustrative example in the architecture doc).
func (c *routerOSClient) AddHotspotUser(name, mac, limitUptime string) error {
	payload := map[string]string{
		"name":         name,
		"mac-address":  mac,
		"limit-uptime": limitUptime,
	}
	return c.do(http.MethodPut, "/rest/ip/hotspot/user", payload, nil)
}

func (c *routerOSClient) GetActiveSessions() ([]ActiveSession, error) {
	var sessions []ActiveSession
	err := c.do(http.MethodGet, "/rest/ip/hotspot/active", nil, &sessions)
	return sessions, err
}
