package app

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

type resetTestMailer struct{ messages []notify.Message }

func (m *resetTestMailer) Send(_ context.Context, msg notify.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}
func TestPasswordResetEmailWorkflowAndGenericResponse(t *testing.T) {
	s := newAppTestServer(t)
	am, _ := auth.New(s.cfg, s.st)
	mailer := &resetTestMailer{}
	a := &App{cfg: s.cfg, st: s.st, auth: am, log: slog.Default(), mailer: mailer, tpl: template.Must(template.New("test").Parse(`{{define "login_help"}}{{.Notice}}{{end}}{{define "password_reset"}}{{.Error}}{{end}}{{define "login"}}{{.Notice}}{{end}}`))}
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err := s.st.SetMailSettings(s.ctx, actor, store.MailSettings{SMTPHost: "localhost", SMTPFromAddr: "noreply@example.test", BaseURL: "https://budget.example.test"}); err != nil {
		t.Fatal(err)
	}
	send := func(email string) string {
		form := url.Values{"email": {email}}
		req := httptest.NewRequest("POST", "/login/help", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.passwordResetRequest(rec, req)
		return rec.Body.String()
	}
	known := send(s.cfg.AdminEmail)
	unknown := send("unknown@example.test")
	if known != unknown || !strings.Contains(known, "If an active account matches") {
		t.Fatalf("account enumeration: %q vs %q", known, unknown)
	}
	if len(mailer.messages) != 1 {
		t.Fatalf("messages=%d", len(mailer.messages))
	}
	match := regexp.MustCompile(`token=([a-f0-9]{64})`).FindStringSubmatch(mailer.messages[0].Body)
	if len(match) != 2 {
		t.Fatal("no strong reset token")
	}
	var stored string
	if err := s.st.DB().QueryRow(`SELECT token_hash FROM password_resets WHERE user_id=?`, actor.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == match[1] {
		t.Fatal("raw token stored")
	}
	post := func(token string) *httptest.ResponseRecorder {
		form := url.Values{"token": {token}, "password": {"Replacement123"}, "confirm_password": {"Replacement123"}}
		req := httptest.NewRequest("POST", "/login/reset", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.passwordResetPost(rec, req)
		return rec
	}
	first := post(match[1])
	if first.Code != 200 || !strings.Contains(first.Body.String(), "password has been reset") {
		t.Fatalf("reset failed: %s", first.Body)
	}
	second := post(match[1])
	if second.Code != 400 {
		t.Fatalf("token reused: %d", second.Code)
	}
	updated, _ := s.st.UserByID(s.ctx, actor.ID)
	if !auth.CheckPassword(updated.PasswordHash, "Replacement123") {
		t.Fatal("new password not persisted")
	}
}
func TestTrustedPasswordResetBase(t *testing.T) {
	for _, raw := range []string{"http://evil.test", "//evil.test", "javascript:alert(1)", "https://user@evil.test", "https://budget.test?x=y"} {
		if trustedResetBase(raw) != "" {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://budget.test", "http://127.0.0.1:8798", "http://localhost:8080"} {
		if trustedResetBase(raw) != raw {
			t.Fatal(raw)
		}
	}
}
func TestLoginPreservesOnlyAuthorizedInternalDestination(t *testing.T) {
	s := newAppTestServer(t)
	for _, tc := range []struct{ next, want string }{{"/audit?entity=role", "/audit?entity=role"}, {"https://evil.test", "/"}, {"//evil.test", "/"}, {"/audit/does-not-exist", "/?login_fallback=1"}, {"/vendors/999999", "/?login_fallback=1"}, {"/requests/999999", "/?login_fallback=1"}} {
		form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}, "next": {tc.next}}
		res := s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
		res.Body.Close()
		if res.Header.Get("Location") != tc.want {
			t.Fatalf("%s -> %s", tc.next, res.Header.Get("Location"))
		}
		if tc.want == "/?login_fallback=1" {
			home := responseBody(t, s.request("GET", tc.want, nil, ""))
			if !strings.Contains(home, loginFallbackNotice) {
				t.Fatal("unavailable destination fallback notice missing")
			}
		}
	}
	hash, _ := auth.HashPassword("Requester123")
	_, err := s.st.CreateUser(s.ctx, "requesterux@example.test", "Requester", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"email": {"requesterux@example.test"}, "password": {"Requester123"}, "next": {"/audit"}}
	res := s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	res.Body.Close()
	if res.Header.Get("Location") != "/?login_fallback=1" {
		t.Fatal("unauthorized destination fallback missing")
	}
	home := responseBody(t, s.request("GET", res.Header.Get("Location"), nil, ""))
	if !strings.Contains(home, loginFallbackNotice) {
		t.Fatal("actual Home HTML does not explain the fallback")
	}
	if strings.Contains(home, `requested page /audit`) {
		t.Fatal("fallback leaked requested route")
	}
}
func TestVendorModesValidationHistoryAndBackupHelp(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err := s.st.SetAppSetting(s.ctx, actor, "payment_modes", "NEFT, UPI, Card"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"name": {"UX Supplier"}, "vendor_type": {"company"}, "status": {"active"}, "gstin": {"BAD"}, "default_payment_mode": {"UPI"}, "upi_id": {"supplier@upi"}}
	bad := s.postForm("/vendors", form)
	body := responseBody(t, bad)
	for _, want := range []string{`href="#v-gst"`, `aria-invalid="true"`, `id="v-gst-error"`, `value="UPI" selected`, `UX Supplier`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	form.Set("gstin", "")
	created := s.postForm("/vendors", form)
	location := created.Header.Get("Location")
	created.Body.Close()
	if !strings.HasPrefix(location, "/vendors/") {
		t.Fatal(location)
	}
	body = responseBody(t, s.request("GET", location, nil, ""))
	for _, tab := range []string{"requests", "payments", "history"} {
		if !strings.Contains(body, location+"/"+tab) {
			t.Fatal("missing tab", tab)
		}
		response := s.request("GET", location+"/"+tab, nil, "")
		requireStatus(t, response, 200)
		response.Body.Close()
	}
	response := s.request("GET", location+"/history", nil, "")
	body = responseBody(t, response)
	if !strings.Contains(body, "Created vendor UX Supplier") {
		t.Fatal("missing vendor history")
	}
	if _, err := store.CreateBackup(s.ctx, store.BackupOptions{DBPath: s.cfg.DBPath, AttachmentDir: s.cfg.AttachmentDir, BackupDir: s.cfg.BackupDir, Now: func() time.Time { return time.Date(2026, 9, 25, 10, 30, 0, 0, time.UTC) }}); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, "/backups", nil, ""))
	for _, want := range []string{"25 Sep 2026", "--restore", "not restore-tested", "Restore safely", "Test Admin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("backup missing %s", want)
		}
	}
	body = responseBody(t, s.request("GET", "/login/help", nil, ""))
	if !strings.Contains(body, "admin@example.test") || !strings.Contains(body, "without email") {
		t.Fatal("offline recovery missing")
	}
}

func TestPaymentEntryUsesConfiguredVendorDefaultAndProtectsDestination(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("ModeUX")
	reqID, vendorID := s.seedVendorRequest(771, actor.ID, actor.ID, head, 10000, "Mode Supplier")
	if _, err := s.st.DB().Exec(`UPDATE vendors SET default_payment_mode='UPI',upi_id='private-supplier@upi',bank_account_number='654321123' WHERE id=?`, vendorID); err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAppSetting(s.ctx, actor, "payment_modes", "NEFT, UPI, Card"); err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReserveRequest(s.ctx, actor, reqID); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request("GET", strconvPath("/payments/new?request=%d", reqID), nil, ""))
	for _, want := range []string{`value="upi" selected>UPI`, `value="neft" >NEFT`, `private-supplier@upi`, `654321123`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(body, `value="cash"`) {
		t.Fatal("unconfigured cash offered")
	}
	// Read privileges, rather than a visible role name, control the bank block.
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var roleID int64
	for _, role := range roles {
		if role.Name == "Admin" {
			roleID = role.ID
		}
	}
	grants, scopes, err := s.st.RolePermissions(s.ctx, roleID)
	if err != nil {
		t.Fatal(err)
	}
	filtered := []store.Grant{}
	for _, g := range grants {
		if g.Resource != "vendor_bank" {
			filtered = append(filtered, g)
		}
	}
	if err = s.st.UpdateRolePermissions(s.ctx, actor, roleID, filtered, scopes); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request("GET", strconvPath("/payments/new?request=%d", reqID), nil, ""))
	if strings.Contains(body, "private-supplier@upi") || strings.Contains(body, "654321123") {
		t.Fatal("bank destination disclosed without permission")
	}
}

func TestVendorHistoryShowsNamedChangesWithoutInventingOldHistory(t *testing.T) {
	entry := store.AuditEntry{Action: "update", BeforeJSON: `{"audit_version":2,"contact_person":"Old contact","name":"Supplier"}`, AfterJSON: `{"audit_version":2,"contact_person":"New contact","name":"Supplier","bank_changed":false}`}
	got := vendorAuditChanges(entry)
	if len(got) != 1 || got[0] != "Contact person: “Old contact” → “New contact”" {
		t.Fatalf("human changes: %v", got)
	}
	old := vendorAuditChanges(store.AuditEntry{Action: "update", BeforeJSON: `{"name":"Supplier"}`, AfterJSON: `{"name":"Supplier","bank_changed":true}`})
	if len(old) != 1 || !strings.Contains(old[0], "not captured") {
		t.Fatalf("invented old history: %v", old)
	}
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	created := s.postForm("/vendors", url.Values{"name": {"History UX"}, "contact_person": {"Old contact"}})
	location := created.Header.Get("Location")
	created.Body.Close()
	changed := s.postForm(location, url.Values{"name": {"History UX"}, "contact_person": {"New contact"}})
	requireStatus(t, changed, 303)
	changed.Body.Close()
	body := responseBody(t, s.request("GET", location+"/history", nil, ""))
	if !strings.Contains(body, "Contact person: “Old contact” → “New contact”") {
		t.Fatal("human contact diff missing from real history")
	}
	if strings.Contains(body, `bank_changed`) {
		t.Fatal("history still shows raw JSON")
	}
}

func TestRoleAuditHTMLShowsPermissionScopeDeltasAndAuthorizedRoleLink(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	actor, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	id, err := s.st.CreateRole(s.ctx, actor, "UX Audit Role", "")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"role_id": {strconvPath("%d", id)}, "name": {"UX Audit Role"}, "perm": {"request:view"}, "scope_request": {"own"}}
	saved := s.postForm("/roles", form)
	requireStatus(t, saved, 303)
	saved.Body.Close()
	body := responseBody(t, s.request("GET", strconvPath("/audit?entity=role&id=%d", id), nil, ""))
	for _, want := range []string{"Payment requests · View: Not granted → Granted", "Payment requests · Record access: None → Own", strconvPath(`href="/roles?role=%d"`, id), "UX Audit Role", "Test Admin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("role audit HTML missing %q", want)
		}
	}
	form.Del("perm")
	form.Del("scope_request")
	saved = s.postForm("/roles", form)
	requireStatus(t, saved, 303)
	saved.Body.Close()
	body = responseBody(t, s.request("GET", strconvPath("/audit?entity=role&id=%d", id), nil, ""))
	for _, want := range []string{"Payment requests · View: Granted → Not granted", "Payment requests · Record access: Own → None"} {
		if !strings.Contains(body, want) {
			t.Fatalf("revocation audit missing %q", want)
		}
	}
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var adminRole int64
	for _, role := range roles {
		if role.Name == "Admin" {
			adminRole = role.ID
		}
	}
	grants, scopes, err := s.st.RolePermissions(s.ctx, adminRole)
	if err != nil {
		t.Fatal(err)
	}
	filtered := []store.Grant{}
	for _, g := range grants {
		if g.Resource != "role" || g.Action != "view" {
			filtered = append(filtered, g)
		}
	}
	limitedRole, e := s.st.CreateRole(s.ctx, actor, "Audit only admin view", "")
	if e != nil {
		t.Fatal(e)
	}
	if err = s.st.UpdateRolePermissions(s.ctx, actor, limitedRole, filtered, scopes); err != nil {
		t.Fatal(err)
	}
	if _, err = s.st.DB().Exec(`DELETE FROM user_roles WHERE user_id=?`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.st.DB().Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, actor.ID, limitedRole); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request("GET", strconvPath("/audit?entity=role&id=%d", id), nil, ""))
	if strings.Contains(body, "Open affected role") {
		t.Fatal("role link shown without role:view")
	}
}
