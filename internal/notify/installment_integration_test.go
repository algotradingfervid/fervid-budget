package notify

import (
	"context"
	"fervidbudget/internal/store"
	"testing"
	"time"
)

func TestInstallmentNotificationUsesCumulativePaidAmount(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	requester := mustUser(t, st, "req-installments@test", "Requester", "data_entry")
	manager := mustUser(t, st, "mgr-installments@test", "Manager", "admin")
	accountant := mustUser(t, st, "acc-installments@test", "Accounts", "admin")
	acc, err := st.UserByID(ctx, accountant)
	must(t, err)
	id := insertRequest(t, st, "PR-2026-009001", "approved", requester, manager, time.Now().UTC(), nil)
	_, err = st.DB().ExecContext(ctx, `UPDATE payment_requests SET amount=1200000, approved_amount=1200000, approved_by=?, approved_at=CURRENT_TIMESTAMP,treatment='recoverable',type='employee_advance',recoverable_category='other' WHERE id=?`, manager, id)
	must(t, err)
	must(t, st.ReserveRequest(ctx, acc, id))
	_, err = historicalSettlement(st, ctx, acc, id, store.PaymentInput{PaidOn: "2026-07-20", Amount: 700000, SubmissionKey: "notify-integration-first"}, "installment", "", nil)
	must(t, err)
	must(t, st.ReserveRequest(ctx, acc, id))
	_, err = historicalSettlement(st, ctx, acc, id, store.PaymentInput{PaidOn: "2026-07-21", Amount: 500000, SubmissionKey: "notify-integration-second"}, "settled", "", nil)
	must(t, err)
	req, err := st.Request(ctx, id)
	must(t, err)
	view, err := NewService(st, &fakeMailer{}).newRequestView(ctx, req, store.MailSettings{})
	must(t, err)
	if view.PaidAmount != 1200000 || view.PaidOn != "2026-07-21" {
		t.Fatalf("notification view paid=%d date=%s, want total 1200000 and latest payout date", view.PaidAmount, view.PaidOn)
	}
}
