package store

import "testing"

func TestMigrationV3CreatesRequestTables(t *testing.T) {
	s := newTestStore(t)
	for _, table := range []string{"payment_requests", "request_attachments", "request_comments", "request_number_seq", "app_settings"} {
		var name string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q missing after migrate: %v", table, err)
		}
	}
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 3 {
		t.Fatalf("user_version = %d, want >= 3", version)
	}
	// A2/A4/A21: the design-system columns exist.
	for _, col := range []string{"vendor_id", "vendor_payee", "short_title", "invoice_no", "invoice_date",
		"expense_date", "advance_reason", "urgency_reason", "attachment_exception_reason",
		"cancel_reason", "recoverable_category", "recoverable_category_id"} {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('payment_requests') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("payment_requests.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	// G9: every user may carry a default approver.
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='default_approver_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users.default_approver_id missing (n=%d err=%v)", n, err)
	}
	// store.Open sets PRAGMA foreign_keys=ON, so requester_id/manager_id must
	// point at a real user or every insert below fails for the wrong reason.
	actorID, err := s.CreateUser(t.Context(), "schema@example.com", "Schema Actor", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	// D1: status has no default and 'draft' is refused by the schema itself.
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-NODEF','budget','vendor_invoice',1,'p',?,?)`, actorID, actorID); err == nil {
		t.Fatal("status accepted a default; D1 requires the writer to name a status")
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DRAFT','draft','budget','vendor_invoice',1,'p',?,?)`, actorID, actorID); err == nil {
		t.Fatal("status 'draft' was accepted; D1 removed drafts entirely")
	}
	// number is UNIQUE
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DUP','pending','budget','vendor_invoice',1,'p',?,?)`, actorID, actorID); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DUP','pending','budget','vendor_invoice',1,'p',?,?)`, actorID, actorID); err == nil {
		t.Fatal("duplicate request number was accepted; UNIQUE(number) missing")
	}
	// D6: the Configuration screen's Phase-2 keys are seeded.
	for key, want := range map[string]string{
		"number_prefix": "PR", "number_year_mode": "calendar", "number_width": "6",
		"require_attachments": "0", "attachment_max_mb": "10",
		"urgency_mode": "reason", "allow_approver_choice": "1",
		"allow_direct_payments": "0",
	} {
		var got string
		if err := s.DB().QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&got); err != nil {
			t.Fatalf("app_settings[%q] missing: %v", key, err)
		}
		if got != want {
			t.Fatalf("app_settings[%q] = %q, want %q", key, got, want)
		}
	}
}
