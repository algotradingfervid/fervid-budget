package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func recoveryFixture(t *testing.T) (*Store, User, int64, int64) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	u, h := seedActorAndHead(t, s, ctx)
	id := seedRecoverableRequestOnly(t, s, ctx, u, h, 0, "PR-REC-1", "completed", "employee_advance", "", "2026-06-01", 1200000, false)
	if _, err := s.DB().Exec(`UPDATE payment_requests SET type='employee_advance' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	payRecoverable(t, s, ctx, u, h, id, "2026-01-02", 700000)
	return s, u, h, id
}
func recoveryInput(token string, amount int64) RecoveryInput {
	return RecoveryInput{Kind: "return", Amount: amount, OccurredOn: "2026-02-01", Reference: "UTR-2026", Note: "Bank receipt recorded in statement REC-01", Token: token}
}

func TestRecoveryLifecyclePreservesPaymentsAndAggregatesInstallments(t *testing.T) {
	s, u, h, id := recoveryFixture(t)
	ctx := context.Background()
	in := recoveryInput("recovery-event-first-token", 200000)
	eventID, err := s.RecordRecovery(ctx, u, id, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.RecordRecovery(ctx, u, id, in)
	if err != nil || again != eventID {
		t.Fatalf("idempotent retry %d %v", again, err)
	}
	rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != 500000 || rows[0].PaidAmount != 700000 || rows[0].RecoveredAmount != 200000 || rows[0].Counterparty != u.Name {
		t.Fatalf("after return %+v", rows)
	}
	payRecoverable(t, s, ctx, u, h, id, "2026-03-01", 500000)
	rows, err = s.RecoverableReport(ctx, RecoverableReportOptions{Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != 1000000 || rows[0].PaidAmount != 1200000 {
		t.Fatalf("installments double counted %+v", rows)
	}
	in = recoveryInput("recovery-event-expense-token", 700000)
	in.Kind = "expense"
	in.OccurredOn = "2026-03-02"
	if _, err = s.RecordRecovery(ctx, u, id, in); err != nil {
		t.Fatal(err)
	}
	in = recoveryInput("recovery-event-adjust-token", 300000)
	in.Kind = "adjustment"
	in.OccurredOn = "2026-03-02"
	if _, err = s.RecordRecovery(ctx, u, id, in); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "recovered", Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != 0 || rows[0].AgeingLabel != "Reconciled" || rows[0].Overdue {
		t.Fatalf("reconciled %+v", rows)
	}
	m, err := s.RecoverableMetrics(ctx, time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC), RecoverableViewerAll())
	if err != nil {
		t.Fatal(err)
	}
	if m.OutstandingCount != 0 || m.PaidThisMonthAmount != 500000 {
		t.Fatalf("metrics %+v", m)
	}
	events, err := s.RecoveryEvents(ctx, u, id)
	if err != nil || len(events) != 3 {
		t.Fatalf("events %+v %v", events, err)
	}
	for _, sql := range []string{`UPDATE recovery_events SET amount=1`, `DELETE FROM recovery_events`} {
		if _, err := s.DB().Exec(sql); err == nil {
			t.Fatal("immutable event mutated")
		}
	}
	var count int
	var paid int64
	if err := s.DB().QueryRow(`SELECT COUNT(*),SUM(amount) FROM payments WHERE request_id=?`, id).Scan(&count, &paid); err != nil {
		t.Fatal(err)
	}
	if count != 2 || paid != 1200000 {
		t.Fatal("recovery mutated payouts")
	}
}

func TestRecoveryRejectsOverageBackdatesMissingEvidenceAndConcurrentOverRecovery(t *testing.T) {
	s, u, _, id := recoveryFixture(t)
	ctx := context.Background()
	for i, change := range []func(*RecoveryInput){func(in *RecoveryInput) { in.Amount = 700001 }, func(in *RecoveryInput) { in.Amount = 0 }, func(in *RecoveryInput) { in.Note = "" }, func(in *RecoveryInput) { in.Reference = "" }, func(in *RecoveryInput) { in.OccurredOn = "2025-01-01" }, func(in *RecoveryInput) { in.OccurredOn = "2099-01-01" }, func(in *RecoveryInput) { in.Kind = "unknown" }} {
		in := recoveryInput(fmt.Sprintf("invalid-event-token-%d", i), 200000)
		change(&in)
		if _, err := s.RecordRecovery(ctx, u, id, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("case%d: %v", i, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.RecordRecovery(ctx, u, id, recoveryInput(fmt.Sprintf("concurrent-event-token-%d", i), 500000))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, refused := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrValidation) {
			refused++
		} else {
			t.Fatalf("unexpected race error %v", err)
		}
	}
	if success != 1 || refused != 1 {
		t.Fatalf("success%d refused%d", success, refused)
	}
}

func TestRecoveryAuthorizationEnforcedAtStoreAndHistory(t *testing.T) {
	s, u, _, id := recoveryFixture(t)
	ctx := context.Background()
	outsiderID, err := s.CreateUser(ctx, "out@example.test", "Outsider", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	outsider, _ := s.UserByID(ctx, outsiderID)
	// Use a custom scoped role, replacing broad Accounts assignment.
	roleID, err := s.CreateRole(ctx, u, "Scoped recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRolePermissions(ctx, u, roleID, []Grant{{"recoverable_report", "view"}, {"payment", "create"}}, []ScopeGrant{{"request", "own"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DELETE FROM user_roles WHERE user_id=?`, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, outsider.ID, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordRecovery(ctx, outsider, id, recoveryInput("outside-scope-event-token", 100000)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outside write %v", err)
	}
	if _, err = s.RecoveryEvents(ctx, outsider, id); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outside history %v", err)
	}
	if _, err = s.DB().Exec(`DELETE FROM role_permissions WHERE role_id=? AND resource='payment'`, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE payment_requests SET requester_id=? WHERE id=?`, outsider.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordRecovery(ctx, outsider, id, recoveryInput("read-only-event-token", 100000)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("view-only write %v", err)
	}
	if _, err = s.RecoveryEvents(ctx, outsider, id); err != nil {
		t.Fatalf("view-only read %v", err)
	}
}

func TestRecoveryDateUsesPaymentLocalCalendarAtMidnight(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 12, 0, 0, time.FixedZone("IST", 19800))
	if !validRecoveryDate("2026-09-26", now) {
		t.Fatal("local today must be allowed although UTC is yesterday")
	}
	if validRecoveryDate("2026-09-27", now) || validRecoveryDate("2026-02-30", now) {
		t.Fatal("future or invalid date allowed")
	}
}

func TestRecoverableLegacyDepositPreservesThirdPartyIdentity(t *testing.T) {
	s, u, _, id := recoveryFixture(t)
	if _, err := s.DB().Exec(`UPDATE payment_requests SET recoverable_category='icd',counterparty='Legacy Deposit Company' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RecoverableReport(context.Background(), RecoverableReportOptions{Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Counterparty != "Legacy Deposit Company" || rows[0].Counterparty == u.Name {
		t.Fatalf("legacy deposit identity %+v", rows)
	}
}

func TestRecoverableNamedDepositPayeeIsCounterpartyFallback(t *testing.T) {
	s, _, _, id := recoveryFixture(t)
	if _, err := s.DB().Exec(`UPDATE payment_requests SET type='recoverable',recoverable_category='emd',counterparty='',vendor_payee='Metro Tender Authority' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RecoverableReport(context.Background(), RecoverableReportOptions{Query: "Metro Tender", Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Counterparty != "Metro Tender Authority" {
		t.Fatalf("named payee fallback %+v", rows)
	}
	rows, err = s.RecoverableReport(context.Background(), RecoverableReportOptions{Counterparty: "Metro Tender Authority", Viewer: RecoverableViewerAll()})
	if err != nil || len(rows) != 1 {
		t.Fatalf("counterparty drilldown %+v %v", rows, err)
	}
}
