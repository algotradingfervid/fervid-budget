package notify

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent []Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "notify-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testActor(t *testing.T, st *store.Store) store.User {
	t.Helper()
	ctx := context.Background()
	id, err := st.CreateUser(ctx, "admin@notify.test", "Admin", "h", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.UserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustUser(t *testing.T, st *store.Store, email, name, role string) int64 {
	t.Helper()
	id, err := st.CreateUser(context.Background(), email, name, "h", role, true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// grantAccounts puts the given users on a role holding payment:process, which
// is how "the Accounts group" resolves to real people.
func grantAccounts(t *testing.T, st *store.Store, userIDs ...int64) {
	t.Helper()
	ctx := context.Background()
	res, err := st.DB().ExecContext(ctx, `INSERT INTO roles(name,is_system) VALUES('Notify Accounts',1)`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, "payment", "process"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range userIDs {
		if _, err := st.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID); err != nil {
			t.Fatal(err)
		}
	}
}

func enableEvent(t *testing.T, st *store.Store, actor store.User, in store.NotificationSetting) {
	t.Helper()
	if err := st.SetNotificationSetting(context.Background(), actor, in); err != nil {
		t.Fatal(err)
	}
}

func insertRequest(t *testing.T, st *store.Store, number, status string, requesterID, managerID int64, submittedAt time.Time, processingAt *time.Time) int64 {
	t.Helper()
	res, err := st.DB().ExecContext(context.Background(),
		`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,vendor_payee,requester_id,manager_id,submitted_at,processing_at,urgent)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,0)`,
		number, status, "budget", "vendor_invoice", int64(500000), "Rent", "Acme", requesterID, managerID, submittedAt.UTC(), processingAt)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}

func countOccurrences(haystack, needle string) int {
	return strings.Count(haystack, needle)
}
