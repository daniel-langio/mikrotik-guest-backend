package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Pricing: 100 Ar buys 3 minutes. Amounts must be whole multiples so price
// and duration convert both ways without rounding.
const (
	pricePerUnit = 100
	minsPerUnit  = 3
	maxAmount    = 50000
)

func durationFor(amount int) (string, bool) {
	if amount < pricePerUnit || amount > maxAmount || amount%pricePerUnit != 0 {
		return "", false
	}
	m := amount / pricePerUnit * minsPerUnit
	return fmt.Sprintf("%d:%02d:00", m/60, m%60), true
}

var e164 = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// normalizePhone accepts "034 12 345 67", "261341234567" or "+261341234567".
func normalizePhone(s string) (string, bool) {
	s = strings.NewReplacer(" ", "", "-", "", ".", "").Replace(s)
	switch {
	case strings.HasPrefix(s, "0") && len(s) == 10:
		s = "+261" + s[1:]
	case strings.HasPrefix(s, "261"):
		s = "+" + s
	}
	return s, e164.MatchString(s)
}

// refFormats are the real Transaction-ID shapes per provider. ifay keeps a
// claim VERIFIED when its ref was already verified, and this receiver number
// also holds test/manual payments with refs like "0", "1" or "REF" - without a
// shape check a guest could claim one of those for instant free access.
var refFormats = map[string]*regexp.Regexp{
	"ORANGE_MONEY": regexp.MustCompile(`^[A-Z]{2}\d{6}\.\d{4}\.[A-Z]\d{5}$`),
	"MVOLA":        regexp.MustCompile(`^\d{8,12}$`),
}

func (a *App) payData(sess PendingSession) map[string]any {
	d := map[string]any{"ID": sess.ID, "Username": sess.Username, "Price": pricePerUnit, "Mins": minsPerUnit,
		"Max": maxAmount}
	if a.ifay != nil {
		d["Receiver"] = a.ifay.receiver
	}
	if ref, claimID, _, err := getClaim(a.db, sess.ID); err == nil && claimID != "" {
		d["Waiting"], d["Ref"] = true, ref
	}
	d["MAC"] = sess.MACAddress
	if sess.Status == "pending" && serviceDisabled(a.db) {
		d["Disabled"], d["Video"] = true, pickVideo(a.db)
	}
	return d
}

func (a *App) payForm(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	sess, err := getSession(a.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	d := a.payData(sess)
	delete(d, "Waiting")
	d["ShowForm"] = true
	render(w, r, "pay.html", d)
}

// pay files the guest's claim with ifay. Approval then needs no human: once
// porofo reports the matching SMS, welcomeStatus sees VERIFIED and grants.
func (a *App) pay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	sess, err := getSession(a.db, id)
	if err != nil || a.ifay == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d := a.payData(sess)
	if d["Disabled"] == true { // service switched off: no new claims
		render(w, r, "pay.html", d)
		return
	}
	d["FPhone"], d["FRef"] = r.FormValue("phone"), r.FormValue("ref")
	lang := langOf(r)
	fail := func(msg string) {
		d["Error"] = msg
		d["ShowForm"] = true
		delete(d, "Waiting")
		render(w, r, "pay.html", d)
	}

	amount, _ := strconv.Atoi(r.FormValue("amount"))
	duration, ok := durationFor(amount)
	if !ok {
		fail(tr(lang, "err.amount", pricePerUnit, maxAmount))
		return
	}
	provider := r.FormValue("provider")
	if provider != "MVOLA" && provider != "ORANGE_MONEY" {
		fail(tr(lang, "err.provider"))
		return
	}
	phone, ok := normalizePhone(r.FormValue("phone"))
	if !ok {
		fail(tr(lang, "err.phone"))
		return
	}
	ref := strings.ToUpper(strings.TrimSpace(r.FormValue("ref")))
	if !refFormats[provider].MatchString(ref) {
		fail(tr(lang, "err.ref_format", map[string]string{"MVOLA": "MVola", "ORANGE_MONEY": "Orange Money"}[provider]))
		return
	}

	switch err := reserveRef(a.db, id, provider, ref, amount); {
	case isUniqueViolation(err):
		fail(tr(lang, "err.ref_used"))
		return
	case errors.Is(err, sql.ErrNoRows):
		fail(tr(lang, "err.not_pending"))
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, err := a.ifay.Claim(phone, amount, provider, ref)
	if err != nil {
		clearClaim(a.db, id)
		fail(tr(lang, "err.ifay", err.Error()))
		return
	}
	recordClaim(a.db, p.ID)
	if err := setClaimID(a.db, id, p.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d = a.payData(sess)
	if p.Status == "VERIFIED" && a.grant(sess, duration, "payment") == nil {
		d["Accepted"] = true
	}
	render(w, r, "pay.html", d)
}

// grant authorizes the device and marks the session approved. Safe to call
// repeatedly (concurrent polls): approveSession only flips 'pending' rows.
func (a *App) grant(sess PendingSession, duration, actor string) error {
	if err := a.router(sess.Router).AuthorizeDevice(sess.MACAddress); err != nil {
		return err
	}
	return approveSession(a.db, sess.ID, duration, actor)
}

// checkClaim grants access to a pending session whose ifay claim is VERIFIED.
func (a *App) checkClaim(sess PendingSession) bool {
	if a.ifay == nil || sess.Status != "pending" || serviceDisabled(a.db) {
		return false
	}
	_, claimID, amount, err := getClaim(a.db, sess.ID)
	duration, ok := durationFor(amount)
	if err != nil || claimID == "" || !ok {
		return false
	}
	// ponytail: one ifay GET per guest poll (2s); cache or batch if guests >> 50.
	if st, err := a.ifay.Status(claimID); err != nil || st != "VERIFIED" {
		return false
	}
	return a.grant(sess, duration, "payment") == nil
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
