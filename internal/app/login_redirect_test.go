package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A fresh sign-in (no earlier session cookie) with ?next= pointing at a
// request, recoverable or payment the user may open must land on that page.
// The record check has to use the user who just signed in: the sign-in POST
// itself carries no signed-in user.
func TestLoginNextOpensVisibleRecordOnFreshSignIn(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("login-next")
	reqID := s.seedPendingRequest(1, admin.ID, admin.ID, headID, 1000)
	recID := s.seedRecoverableRequest(2, admin.ID, admin.ID, "icd", "Counterparty", 1000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	_, payID := s.settleOneRequest(3, headID, 500000, "5000.00", "settled", time.Now().Format("2006-01-02"))
	for _, next := range []string{
		fmt.Sprintf("/requests/%d", reqID),
		fmt.Sprintf("/recoverables/%d", recID),
		fmt.Sprintf("/payments/%d", payID),
	} {
		// The same server, but a client with no cookies: no earlier session.
		client := *s.client
		client.Jar = nil
		form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}, "next": {next}}
		req, err := http.NewRequest(http.MethodPost, s.server.URL+"/login", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Location"); got != next {
			t.Fatalf("sign-in with next=%s redirected to %q, want %q", next, got, next)
		}
	}
}
