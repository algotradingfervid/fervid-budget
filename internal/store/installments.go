package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// upInstallments preserves every historical payment and settlement. Only the
// one-payment index is relaxed; existing writeoffs are never reopened.
func upInstallments(tx *sql.Tx) error {
	has, err := columnExists(tx, "payments", "submission_key")
	if err != nil {
		return err
	}
	if !has {
		if _, err = tx.Exec(`ALTER TABLE payments ADD COLUMN submission_key TEXT`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`DROP INDEX IF EXISTS idx_payments_request;
 CREATE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
 CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_submission ON payments(submission_key) WHERE submission_key IS NOT NULL;`)
	return err
}

// RequestPayments returns the complete append-only payment history, oldest first.
func (s *Store) RequestPayments(ctx context.Context, requestID int64) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM payments WHERE request_id=? ORDER BY id`, requestID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []Payment
	for _, id := range ids {
		p, err := s.Payment(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Duplicate invoice matches are vendor + normalized invoice + invoice year.
// They are warnings with a recorded exception, never an unoverridable ban.
func requireDuplicateReasonTx(ctx context.Context, tx *sql.Tx, in RequestInput, excludeID int64) error {
	if in.Type != "vendor_invoice" || in.VendorID <= 0 || strings.TrimSpace(in.InvoiceNo) == "" {
		return nil
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests WHERE id<>? AND vendor_id=? AND lower(trim(invoice_no))=lower(trim(?)) AND substr(invoice_date,1,4)=substr(?,1,4) AND status NOT IN ('withdrawn','rejected','cancelled')`, excludeID, in.VendorID, in.InvoiceNo, in.InvoiceDate).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 && len(strings.TrimSpace(in.DuplicateReason)) < 10 {
		return fmt.Errorf("%w: this vendor invoice already exists; review the matching request and give an override reason of at least 10 characters", ErrValidation)
	}
	return nil
}

// DuplicateInvoices returns only matches the viewer may read. The write guard
// still catches matches outside their scope without disclosing another record.
func (s *Store) DuplicateInvoices(ctx context.Context, in RequestInput, opts RequestListOptions) ([]Request, error) {
	opts.Type = "vendor_invoice"
	opts.Bucket = "all"
	opts.Limit = RequestsUnlimited
	all, err := s.ListRequests(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out []Request
	for _, r := range all {
		if r.VendorID != nil && *r.VendorID == in.VendorID && strings.EqualFold(strings.TrimSpace(r.InvoiceNo), strings.TrimSpace(in.InvoiceNo)) && len(r.InvoiceDate) >= 4 && len(in.InvoiceDate) >= 4 && r.InvoiceDate[:4] == in.InvoiceDate[:4] && r.Status != "withdrawn" && r.Status != "rejected" && r.Status != "cancelled" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Store) PaymentBySubmission(ctx context.Context, requestID, actorID int64, key string) (Payment, error) {
	if key == "" {
		return Payment{}, ErrNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM payments WHERE request_id=? AND entered_by=? AND submission_key=?`, requestID, actorID, key).Scan(&id)
	if err == sql.ErrNoRows {
		return Payment{}, ErrNotFound
	}
	if err != nil {
		return Payment{}, err
	}
	return s.Payment(ctx, id)
}

// ContinuePartial returns a manager-reviewed shortfall to the existing approval.
// It never raises the approved ceiling or alters any recorded payment.
func (s *Store) ContinuePartial(ctx context.Context, actor User, id int64, note string) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w: explain when the remaining balance should be paid", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, id); err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "partial_review" {
		return fmt.Errorf("%w: only a partial review can return to payment", ErrValidation)
	}
	ceiling := before.Amount
	if before.ApprovedAmount != nil {
		ceiling = *before.ApprovedAmount
	}
	if before.PaidAmount >= ceiling {
		return fmt.Errorf("%w: there is no approved balance left to pay", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved',processing_by=NULL,processing_at=NULL,concern_open=0,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='partial_review'`, id)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "continue_payment", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " kept the approved balance payable: " + strings.TrimSpace(note)}); err != nil {
		return err
	}
	return tx.Commit()
}
