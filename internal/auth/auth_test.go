package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

func TestValidatePassword(t *testing.T) {
	for _, password := range []string{"short1", "onlyletters", "12345678"} {
		if err := ValidatePassword(password); err == nil {
			t.Fatalf("ValidatePassword(%q) unexpectedly succeeded", password)
		}
	}
	if err := ValidatePassword("budget2026"); err != nil {
		t.Fatalf("ValidatePassword valid password: %v", err)
	}
}

func newTestManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	manager, err := New(config.Config{SessionKey: "auth-test-session-key"}, st)
	if err != nil {
		t.Fatal(err)
	}
	return manager, st
}

func createTestUser(t *testing.T, st *store.Store, email, role string, active bool) store.User {
	t.Helper()
	hash, err := HashPassword("Account123")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateUser(t.Context(), email, "Test User", hash, role, active)
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.UserByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func cookieByName(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response did not include %s cookie", name)
	return nil
}

func authenticatedRequest(t *testing.T, manager *Manager, user store.User, method, target string) *http.Request {
	t.Helper()
	loginRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	loginResponse := httptest.NewRecorder()
	manager.Login(loginResponse, loginRequest, user)
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(cookieByName(t, loginResponse, sessionCookie))
	request.AddCookie(cookieByName(t, loginResponse, csrfCookie))
	return request
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("Budget123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "Budget123") {
		t.Fatal("valid password did not match its hash")
	}
	if CheckPassword(hash, "Wrong123") {
		t.Fatal("wrong password matched the hash")
	}
	if _, err := HashPassword("weak"); err == nil {
		t.Fatal("HashPassword accepted a weak password")
	}
}

func TestSessionMiddlewareAndPermissions(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "admin@example.test", "admin", true)
	entry := createTestUser(t, st, "entry@example.test", "data_entry", true)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r).ID == 0 {
			t.Fatal("authenticated user was not stored in the request context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, testCase := range []struct {
		name       string
		user       store.User
		object     string
		action     string
		wantStatus int
	}{
		{name: "admin wildcard", user: admin, object: "user", action: "update", wantStatus: http.StatusNoContent},
		{name: "data entry create payment", user: entry, object: "payment", action: "create", wantStatus: http.StatusNoContent},
		{name: "data entry denied users", user: entry, object: "user", action: "read", wantStatus: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := authenticatedRequest(t, manager, testCase.user, http.MethodGet, "/")
			response := httptest.NewRecorder()
			manager.Middleware(manager.RequirePermission(testCase.object, testCase.action, okHandler)).ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
		})
	}

	response := httptest.NewRecorder()
	manager.Middleware(manager.RequireLogin(okHandler)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous request = %d %q, want login redirect", response.Code, response.Header().Get("Location"))
	}
}

func TestCanAndPermissionsSnapshotAgree(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "perm-admin@example.test", "admin", true)
	entry := createTestUser(t, st, "perm-entry@example.test", "data_entry", true)

	for _, testCase := range []struct {
		name     string
		user     store.User
		resource string
		action   string
		want     bool
	}{
		{name: "admin wildcard resource", user: admin, resource: "role", action: "edit", want: true},
		{name: "admin payment create", user: admin, resource: "payment", action: "create", want: true},
		{name: "data entry payment create", user: entry, resource: "payment", action: "create", want: true},
		{name: "data entry report export", user: entry, resource: "report", action: "export", want: true},
		{name: "data entry role edit denied", user: entry, resource: "role", action: "edit", want: false},
		{name: "data entry unknown resource denied", user: entry, resource: "sprocket", action: "read", want: false},
		{name: "data entry known resource wrong action", user: entry, resource: "payment", action: "void", want: false},
		{name: "anonymous denied", user: store.User{}, resource: "payment", action: "read", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := manager.Can(testCase.user, testCase.resource, testCase.action); got != testCase.want {
				t.Fatalf("Can(%q, %q, %q) = %v, want %v", testCase.user.Role, testCase.resource, testCase.action, got, testCase.want)
			}
			perms := manager.Permissions(testCase.user)
			if perms == nil {
				t.Fatal("Permissions returned a nil permission set")
			}
			if got := perms.Can(testCase.resource, testCase.action); got != testCase.want {
				t.Fatalf("Permissions(%q).Can(%q, %q) = %v, want %v", testCase.user.Role, testCase.resource, testCase.action, got, testCase.want)
			}
		})
	}

	if got := manager.Permissions(entry).Scope("payment"); got != store.ScopeAll {
		t.Fatalf("data_entry Scope(payment) = %q, want %q", got, store.ScopeAll)
	}
	if got := manager.Permissions(entry).Scope("role"); got != "" {
		t.Fatalf("data_entry Scope(role) = %q, want empty scope", got)
	}
	if got := manager.Permissions(admin).Scope("role"); got != store.ScopeAll {
		t.Fatalf("admin Scope(role) = %q, want %q", got, store.ScopeAll)
	}
	if got := manager.Permissions(store.User{}).Scope("payment"); got != "" {
		t.Fatalf("anonymous Scope(payment) = %q, want empty scope", got)
	}
}

func TestSessionRejectsTamperingExpiryAndInactiveUsers(t *testing.T) {
	manager, st := newTestManager(t)
	user := createTestUser(t, st, "session@example.test", "data_entry", true)

	valid := authenticatedRequest(t, manager, user, http.MethodGet, "/")
	cookie, _ := valid.Cookie(sessionCookie)
	cookie.Value = "A" + cookie.Value[1:]
	tampered := httptest.NewRequest(http.MethodGet, "/", nil)
	tampered.AddCookie(cookie)
	if _, ok := manager.userFromRequest(tampered); ok {
		t.Fatal("tampered session was accepted")
	}

	expiredPayload := fmt.Sprintf("%d:%d", user.ID, time.Now().Add(-time.Minute).Unix())
	expired := httptest.NewRequest(http.MethodGet, "/", nil)
	expired.AddCookie(&http.Cookie{
		Name:  sessionCookie,
		Value: base64.RawURLEncoding.EncodeToString([]byte(expiredPayload + ":" + manager.sign(expiredPayload))),
	})
	if _, ok := manager.userFromRequest(expired); ok {
		t.Fatal("expired session was accepted")
	}

	if err := st.UpdateUser(t.Context(), user.ID, user.Name, user.Role, false, ""); err != nil {
		t.Fatal(err)
	}
	inactive := authenticatedRequest(t, manager, user, http.MethodGet, "/")
	if _, ok := manager.userFromRequest(inactive); ok {
		t.Fatal("inactive user's session was accepted")
	}

	for _, value := range []string{"not-base64", base64.RawURLEncoding.EncodeToString([]byte("bad:shape"))} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
		if _, ok := manager.userFromRequest(request); ok {
			t.Fatalf("invalid session %q was accepted", value)
		}
	}
}

func TestCSRFAndLogoutLifecycle(t *testing.T) {
	manager, _ := newTestManager(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	token := manager.EnsureCSRF(response, request)
	if len(token) != 32 {
		t.Fatalf("CSRF token length = %d, want 32", len(token))
	}
	csrf := cookieByName(t, response, csrfCookie)

	safe := httptest.NewRequest(http.MethodGet, "/", nil)
	if !manager.CheckCSRF(safe) {
		t.Fatal("safe request was rejected by CSRF validation")
	}
	missing := httptest.NewRequest(http.MethodPost, "/", nil)
	if manager.CheckCSRF(missing) {
		t.Fatal("POST without token was accepted")
	}
	header := httptest.NewRequest(http.MethodPost, "/", nil)
	header.AddCookie(csrf)
	header.Header.Set("X-CSRF-Token", token)
	if !manager.CheckCSRF(header) {
		t.Fatal("matching CSRF header was rejected")
	}

	protected := manager.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rejected := httptest.NewRecorder()
	protected.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/", nil))
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", rejected.Code)
	}
	accepted := httptest.NewRecorder()
	protected.ServeHTTP(accepted, header)
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF status = %d, want 204", accepted.Code)
	}

	logout := httptest.NewRecorder()
	manager.Logout(logout)
	session := cookieByName(t, logout, sessionCookie)
	if session.MaxAge != -1 || session.Value != "" {
		t.Fatalf("logout cookie = %#v, want expired empty session", session)
	}
}
