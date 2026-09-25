package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafeReturnPath(t *testing.T) {
	for _, input := range []string{"https://evil.test/a", "//evil.test", "/%2fexample.test", "/\\evil.test", "/login?next=/audit", "/logout", "/a%0d%0aX:y", "not-local"} {
		if got := SafeReturnPath(input); got != "/" {
			t.Errorf("%q -> %q", input, got)
		}
	}
	if got := SafeReturnPath("/audit?entity=role&id=3"); got != "/audit?entity=role&id=3" {
		t.Fatal(got)
	}
}
func TestRequireLoginPreservesGetDestination(t *testing.T) {
	manager, _ := newTestManager(t)
	h := manager.RequireLogin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated passed") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/audit?entity=role", nil))
	if got := rec.Header().Get("Location"); got != "/login?next=%2Faudit%3Fentity%3Drole" {
		t.Fatal(got)
	}
}
func TestPasswordChangeInvalidatesExistingSession(t *testing.T) {
	manager, st := newTestManager(t)
	u := createTestUser(t, st, "reset@example.test", "admin", true)
	request := authenticatedRequest(t, manager, u, http.MethodGet, "/")
	if _, ok := manager.userFromRequest(request); !ok {
		t.Fatal("fresh session rejected")
	}
	hash, err := HashPassword("Replacement123")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateUser(t.Context(), u.ID, u.Name, u.Role, true, hash); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.userFromRequest(request); ok {
		t.Fatal("old session remains valid after password reset")
	}
}
