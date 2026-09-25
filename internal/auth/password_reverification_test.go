package auth

import (
	"strings"
	"testing"
)

func TestReverifyPasswordValidationMatchesBcryptByteLimit(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 72) + "1", strings.Repeat("界", 24) + "A1"} {
		if err := ValidatePassword(value); err == nil {
			t.Fatalf("accepted %d-byte password that bcrypt cannot hash", len(value))
		}
	}
	if err := ValidatePassword(strings.Repeat("a", 71) + "1"); err != nil {
		t.Fatal(err)
	}
}

func TestReverifyReactivationDoesNotReviveOldSession(t *testing.T) {
	manager, st := newTestManager(t)
	u := createTestUser(t, st, "reactivate@example.invalid", "data_entry", true)
	request := authenticatedRequest(t, manager, u, "GET", "/")
	if err := st.UpdateUser(t.Context(), u.ID, u.Name, u.Role, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.userFromRequest(request); ok {
		t.Fatal("deactivated session accepted")
	}
	if err := st.UpdateUser(t.Context(), u.ID, u.Name, u.Role, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.userFromRequest(request); ok {
		t.Fatal("reactivation revived a session issued before deactivation")
	}
}
