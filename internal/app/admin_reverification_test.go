package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func TestReverifyBackupRestoreCommandKeepsAttachmentDownloadWorking(t *testing.T) {
	source := newAppTestServer(t)
	actor, head := source.seedHead("Restore Attachment")
	paymentID, err := source.st.CreatePayment(source.ctx, actor, store.PaymentInput{HeadID: head, PaidOn: "2026-09-20", Amount: 32145, VendorPayee: "Synthetic vendor", PaymentMode: "upi"})
	if err != nil {
		t.Fatal(err)
	}
	proof := []byte("Synthetic receipt for restore verification\nAmount: INR 321.45\n")
	stored := filepath.Join(source.cfg.AttachmentDir, "synthetic-receipt.txt")
	if err = os.WriteFile(stored, proof, 0600); err != nil {
		t.Fatal(err)
	}
	if err = source.st.AddAttachment(source.ctx, actor, paymentID, "synthetic-receipt.txt", stored, "text/plain", int64(len(proof))); err != nil {
		t.Fatal(err)
	}
	atts, err := source.st.Attachments(source.ctx, paymentID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachment fixture: %v %v", atts, err)
	}
	info, err := store.Backup(source.ctx, source.cfg.DBPath, source.cfg.AttachmentDir, source.cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "stage with ' quote and $ dollar")
	cfg := source.cfg
	cfg.DBPath = filepath.Join(destination, "fervid.db")
	cfg.AttachmentDir = filepath.Join(destination, "attachments")
	a := &App{cfg: cfg}
	row := a.backupRows([]string{filepath.Base(info.Path)})[0]
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", row.RestoreCommand)
	cmd.Dir = filepath.Clean(filepath.Join(cwd, "../.."))
	cmd.Env = append(os.Environ(), "FERVID_SMTP_PASSWORD=")
	output, err := cmd.CombinedOutput()
	t.Logf("Executed generated CLI restore into isolated staging paths: %s", output)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(cfg.AttachmentDir, "synthetic-receipt.txt"))
	if err != nil || !bytes.Equal(copied, proof) {
		t.Fatalf("restored bytes: %q %v", copied, err)
	}
	restored, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	payment, err := restored.Payment(source.ctx, paymentID)
	if err != nil || payment.Amount != 32145 {
		t.Fatalf("restored payment: %+v %v", payment, err)
	}
	srv, err := New(cfg, restored)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(srv.Handler)
	defer server.Close()
	target := &appTestServer{t: t, ctx: source.ctx, cfg: cfg, st: restored, server: server, client: source.client}
	target.login(cfg.AdminEmail, testAdminPassword)
	response := target.request("GET", fmt.Sprintf("/attachments/%d", atts[0].ID), nil, "")
	requireStatus(t, response, http.StatusOK)
	if got := responseBody(t, response); got != string(proof) {
		t.Fatalf("download after restore=%q", got)
	}
	t.Log("Verified payment value, restored file bytes and authenticated attachment download after executing generated restore command.")
}

func TestReverifyPasswordResetValidationPreservesLinkAndSession(t *testing.T) {
	s := newAppTestServer(t)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	token := strings.Repeat("ab", 32)
	if err := s.st.CreatePasswordReset(s.ctx, actor.ID, resetHash(token), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	tooLong := strings.Repeat("界", 24) + "A1"
	bad := s.postForm("/login/reset", url.Values{"token": {token}, "password": {tooLong}, "confirm_password": {tooLong}})
	requireStatus(t, bad, 422)
	body := responseBody(t, bad)
	if !strings.Contains(body, "at most 72 bytes") || !strings.Contains(body, token) || strings.Contains(body, "already used") {
		t.Fatal("length validation loses or mislabels valid reset link")
	}
	res := s.request("GET", "/audit", nil, "")
	requireStatus(t, res, 200)
	res.Body.Close()
	good := s.postForm("/login/reset", url.Values{"token": {token}, "password": {"ValidReplacement123"}, "confirm_password": {"ValidReplacement123"}})
	requireStatus(t, good, 200)
	good.Body.Close()
	next := s.request("GET", "/audit", nil, "")
	requireStatus(t, next, 303)
	next.Body.Close()
	changed, _ := s.st.UserByID(s.ctx, actor.ID)
	if !auth.CheckPassword(changed.PasswordHash, "ValidReplacement123") {
		t.Fatal("corrected password not usable")
	}
	s.login(s.cfg.AdminEmail, "ValidReplacement123")
	reused := s.postForm("/login/reset", url.Values{"token": {token}, "password": {"OtherReplacement123"}, "confirm_password": {"OtherReplacement123"}})
	requireStatus(t, reused, 400)
	reused.Body.Close()
}

func TestReverifyHistoryPermissionGatesAndOfflineReset(t *testing.T) {
	s := newAppTestServer(t)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	vendorID, err := s.st.CreateVendor(s.ctx, actor, store.VendorInput{Name: "Protected history vendor", ContactPerson: "Private contact", Bank: &store.VendorBank{AccountNumber: "DO-NOT-EXPOSE", UPIID: "private@upi"}})
	if err != nil {
		t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, actor, "Vendor without audit", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.st.UpdateRolePermissions(s.ctx, actor, roleID, []store.Grant{{Resource: "vendor", Action: "view"}}, nil); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("LimitedUser123")
	_, err = s.st.CreateUserWithRoles(s.ctx, "limitedadmin@example.invalid", "Limited admin", hash, "data_entry", true, []int64{roleID})
	if err != nil {
		t.Fatal(err)
	}
	s.login("limitedadmin@example.invalid", "LimitedUser123")
	page := responseBody(t, s.request("GET", fmt.Sprintf("/vendors/%d", vendorID), nil, ""))
	if strings.Contains(page, fmt.Sprintf("/vendors/%d/history", vendorID)) || strings.Contains(page, "DO-NOT-EXPOSE") {
		t.Fatal("history navigation or bank leaked")
	}
	for _, path := range []string{fmt.Sprintf("/vendors/%d/history", vendorID), "/audit?entity=role", "/roles"} {
		res := s.request("GET", path, nil, "")
		requireStatus(t, res, 403)
		res.Body.Close()
	}
	// Public offline recovery does not mint reset credentials or enumerate accounts.
	known := responseBody(t, s.postForm("/login/help", url.Values{"email": {s.cfg.AdminEmail}}))
	unknown := responseBody(t, s.postForm("/login/help", url.Values{"email": {"nobody@example.invalid"}}))
	if known != unknown || !strings.Contains(known, resetNotice) || !strings.Contains(known, "without email") {
		t.Fatal("offline reset confirmation diverges or loses fallback")
	}
	var n int
	if err = s.st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("offline minted reset token: %d %v", n, err)
	}
}

func TestReverifySelectedRoleDestinationIsAvailable(t *testing.T) {
	s := newAppTestServer(t)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	id, err := s.st.CreateRole(s.ctx, actor, "Return role", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ next, want string }{{fmt.Sprintf("/roles?role=%d", id), fmt.Sprintf("/roles?role=%d", id)}, {"/roles?role=999999", "/?login_fallback=1"}, {"/audit?entity=role&action=update&from=2026-09-01", "/audit?entity=role&action=update&from=2026-09-01"}} {
		form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}, "next": {tc.next}}
		response := s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
		response.Body.Close()
		if got := response.Header.Get("Location"); got != tc.want {
			t.Fatalf("return %q got %q want %q", tc.next, got, tc.want)
		}
	}
}

func TestReverifyHTTPReactivationRequiresFreshSession(t *testing.T) {
	s := newAppTestServer(t)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	hash, _ := auth.HashPassword("Reactivation123")
	id, err := s.st.CreateUser(s.ctx, "reactivatehttp@example.invalid", "Reactivation test", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	s.login("reactivatehttp@example.invalid", "Reactivation123")
	base, _ := url.Parse(s.server.URL)
	var captured *http.Cookie
	for _, cookie := range s.client.Jar.Cookies(base) {
		if cookie.Name == "fervid_session" {
			copy := *cookie
			captured = &copy
		}
	}
	if captured == nil {
		t.Fatal("no issued session")
	}
	user, _ := s.st.UserByID(s.ctx, id)
	if user.SessionVersion != 0 {
		t.Fatal("new user did not start at generation0")
	}
	replay := func() int {
		req, _ := http.NewRequest("GET", s.server.URL+"/", nil)
		req.AddCookie(captured)
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if got := replay(); got != 200 {
		t.Fatalf("fresh session=%d", got)
	}
	roles, err := s.st.UserRoles(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	roleIDs := []int64{}
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	in := store.UserSaveInput{ID: id, Name: user.Name, Role: user.Role, RoleIDs: roleIDs, Active: false}
	if err = s.st.SaveUser(s.ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	if got := replay(); got != 303 {
		t.Fatalf("inactive session=%d", got)
	}
	in.Active = true
	if err = s.st.SaveUser(s.ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	if got := replay(); got != 303 {
		t.Fatalf("old session revived after reactivation=%d", got)
	}
	s.login("reactivatehttp@example.invalid", "Reactivation123")
	home := s.request("GET", "/", nil, "")
	requireStatus(t, home, 200)
	home.Body.Close()
	t.Log("HTTP cookie replay: fresh=200; deactivated=303; reactivated old cookie=303; fresh re-login=200.")
}

func TestReverifyBackupManifestRequiresInspection(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "backup-invalid")
	if err := os.MkdirAll(filepath.Join(folder, "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "fervid.db"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.cfg.BackupDir = root
	if row := a.backupRows([]string{"backup-invalid"})[0]; row.Status != "Needs inspection" {
		t.Fatal("missing manifest reported ready", row)
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), []byte(`{"created_at":"not-a-time"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if row := a.backupRows([]string{"backup-invalid"})[0]; row.Status != "Needs inspection" {
		t.Fatal("invalid manifest reported ready", row)
	}
}
