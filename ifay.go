package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Ifay is a minimal client for the ifay payment API (claims side only). porofo,
// running on the receiver's phone, reports the matching SMS to ifay; we only
// file the guest's claim and poll its status.
type Ifay struct {
	baseURL, apiKey, receiver string
	client                    *http.Client
}

type ifayPayment struct {
	ID     string `json:"id"`
	Status string `json:"status"` // PENDING | VERIFIED
	Amount int64  `json:"amount"`
}

// ifayFromEnv returns nil when unconfigured, which leaves the portal on
// manual admin approval.
func ifayFromEnv() *Ifay {
	url, key, recv := os.Getenv("IFAY_URL"), os.Getenv("IFAY_CLIENT_API_KEY"), os.Getenv("IFAY_RECEIVER_PHONE")
	if url == "" || key == "" || recv == "" {
		return nil
	}
	return &Ifay{strings.TrimRight(url, "/"), key, recv, &http.Client{Timeout: 10 * time.Second}}
}

func (c *Ifay) do(method, path string, body any) (ifayPayment, error) {
	var p ifayPayment
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, rd)
	if err != nil {
		return p, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return p, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		var e struct{ Message string }
		json.Unmarshal(data, &e)
		return p, fmt.Errorf("ifay %d: %s", resp.StatusCode, e.Message)
	}
	return p, json.Unmarshal(data, &p)
}

// Claim records that senderPhone paid amount Ar under ref. Returns VERIFIED
// at once if porofo already reported that ref.
func (c *Ifay) Claim(senderPhone string, amount int, provider, ref string) (ifayPayment, error) {
	return c.do("POST", "/payments/claims", map[string]any{
		"senderPhone": senderPhone, "receiverPhone": c.receiver,
		"amount": amount, "type": provider, "pspRef": ref,
	})
}

func (c *Ifay) Status(id string) (string, error) {
	p, err := c.do("GET", "/payments/claims/"+id, nil)
	return p.Status, err
}
