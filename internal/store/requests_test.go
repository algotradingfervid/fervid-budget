package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"fervidbudget/internal/money"
)

func TestNextRequestNumberIsMonotonicPerYear(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := NextRequestNumber(tx, "2026")
		if err != nil {
			t.Fatalf("NextRequestNumber: %v", err)
		}
		got = append(got, n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []string{"PR-2026-000001", "PR-2026-000002", "PR-2026-000003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("number[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	tx2, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	n, err := NextRequestNumber(tx2, "2027")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "PR-2027-000001") {
		t.Fatalf("new-year number = %q, want PR-2027-000001", n)
	}
}

// A20/D6: the Configuration numbering fieldset actually drives the format.
func TestNextRequestNumberHonoursConfiguredFormat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='REQ' WHERE key='number_prefix'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='4' WHERE key='number_width'`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n, err := NextRequestNumber(tx, "2026")
	if err != nil {
		t.Fatal(err)
	}
	if n != "REQ-2026-0001" {
		t.Fatalf("configured number = %q, want REQ-2026-0001", n)
	}
}

func TestRequestNumberYearSegmentModes(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	march := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if y, err := requestNumberYear(tx, march); err != nil || y != "2026" {
		t.Fatalf("calendar year = %q, %v; want 2026", y, err)
	}
	if _, err := tx.Exec(`UPDATE app_settings SET value='financial' WHERE key='number_year_mode'`); err != nil {
		t.Fatal(err)
	}
	if y, err := requestNumberYear(tx, march); err != nil || y != "2025-26" {
		t.Fatalf("financial year (March) = %q, %v; want 2025-26", y, err)
	}
	if y, err := requestNumberYear(tx, april); err != nil || y != "2026-27" {
		t.Fatalf("financial year (April) = %q, %v; want 2026-27", y, err)
	}
}

func TestValidateRequestInputPerType(t *testing.T) {
	base := func(mut func(*RequestInput)) RequestInput {
		in := RequestInput{
			Treatment: "budget", Type: "vendor_invoice", ShortTitle: "July switchgear",
			ProjectID: 1, HeadID: 2, Amount: 1000, Purpose: "buy", ManagerID: 7,
			VendorID: 3, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18",
		}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	cases := []struct {
		name string
		in   RequestInput
		ok   bool
	}{
		{"vendor_invoice ok", base(nil), true},
		{"vendor_invoice needs project", base(func(i *RequestInput) { i.ProjectID = 0 }), false},
		{"vendor_invoice needs a vendor row, not free text", base(func(i *RequestInput) { i.VendorID = 0; i.VendorPayee = "Acme" }), false},
		{"vendor_invoice needs invoice number", base(func(i *RequestInput) { i.InvoiceNo = "" }), false},
		{"vendor_invoice needs invoice date", base(func(i *RequestInput) { i.InvoiceDate = "" }), false},
		{"vendor_invoice rejects a malformed invoice date", base(func(i *RequestInput) { i.InvoiceDate = "18-07-2026" }), false},
		{"short title is required", base(func(i *RequestInput) { i.ShortTitle = "  " }), false},
		{"vendor_advance ok", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
			i.AdvanceReason = "40% booking against PO-2026-0417"
		}), true},
		{"vendor_advance needs a reason", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
		}), false},
		{"vendor_advance needs a vendor", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate, i.VendorID = "vendor_advance", "", "", 0
			i.AdvanceReason = "booking"
		}), false},
		{"reimbursement ok without a vendor", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
			i.ExpenseDate = "2026-07-21"
		}), true},
		{"reimbursement needs the expense date", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
		}), false},
		{"employee_advance budget needs head", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate, i.HeadID = "employee_advance", 0, "", "", 0
			i.AdvanceReason = "site mobilisation cash"
		}), false},
		{"employee_advance recoverable needs return date", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", RepaymentNotes: "monthly"}
		}), false},
		{"employee_advance recoverable ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "monthly"}
		}), true},
		{"recoverable EMD ok with a project", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd", ProjectID: 4,
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), true},
		{"recoverable EMD without a project is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), false},
		{"recoverable ICD without a counterparty is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), false},
		{"recoverable ICD with a counterparty ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				Counterparty: "Meridian Holdings Pvt Ltd", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), true},
		{"unknown recoverable category", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Mystery",
				Amount: 1000, Purpose: "x", ManagerID: 7, RecoverableCategory: "mystery",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "n"}
		}), false},
		{"unknown type", base(func(i *RequestInput) { i.Type = "mystery" }), false},
		{"no manager", base(func(i *RequestInput) { i.ManagerID = 0 }), false},
		{"no amount", base(func(i *RequestInput) { i.Amount = 0 }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRequestInput(c.in, recoverableCategoryRules)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

// A5/D1: 'draft' is gone; cancellation_requested and cancelled are in.
func TestCanTransition(t *testing.T) {
	legal := map[[2]string]bool{
		{"returned", "pending"}: true,
		{"pending", "approved"}: true, {"pending", "returned"}: true,
		{"pending", "rejected"}: true, {"pending", "withdrawn"}: true,
		{"approved", "cancellation_requested"}: true, {"approved", "cancelled"}: true,
		{"cancellation_requested", "cancelled"}: true, {"cancellation_requested", "approved"}: true,
	}
	all := []string{"draft", "pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"}
	for _, from := range all {
		for _, to := range all {
			want := legal[[2]string{from, to}]
			if got := canTransition(from, to); got != want {
				t.Fatalf("canTransition(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
	// L1 (inverted): 'draft' is not a status this system knows.
	if requestStatuses["draft"] {
		t.Fatal("draft is present in the status enum; D1 removed it")
	}
	for _, s := range []string{"pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"} {
		if !requestStatuses[s] {
			t.Fatalf("status %q missing from the enum", s)
		}
	}
}

func seedRequestActors(t *testing.T, s *Store, ctx context.Context) (requester User, manager User, headID int64) {
	t.Helper()
	rid, err := s.CreateUser(ctx, "req@example.com", "Rhea Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := s.CreateUser(ctx, "mgr@example.com", "Manav Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	requester, _ = s.UserByID(ctx, rid)
	manager, _ = s.UserByID(ctx, mid)
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	headID, err = s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	return requester, manager, headID
}

// seedTestVendor inserts a Phase-1V vendor row directly; Phase 2 only needs its id.
func seedTestVendor(t *testing.T, s *Store, ctx context.Context, name string) int64 {
	t.Helper()
	res, err := s.DB().ExecContext(ctx, `INSERT INTO vendors(name,vendor_type,status,gstin) VALUES(?,'company','active','29AABCS1429B1ZQ')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateRequestIsAtomicCreateAndSubmit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	id, err := s.CreateRequest(ctx, req, RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Team lunch",
		ProjectID: 1, HeadID: headID, Amount: 50000, Purpose: "team lunch",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID, VendorPayee: "ignored",
		Urgent: true, UrgencyReason: "card bill due Monday",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	got, err := s.Request(ctx, id)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	// D1: created already pending, numbered, and stamped in one operation.
	if got.Status != "pending" {
		t.Fatalf("status = %q, want pending (D1: there are no drafts)", got.Status)
	}
	if got.SubmittedAt == nil {
		t.Fatal("submitted_at not stamped; create and submit are one operation")
	}
	wantPrefix := "PR-" + time.Now().UTC().Format("2006") + "-"
	if !strings.HasPrefix(got.Number, wantPrefix) {
		t.Fatalf("number = %q, want prefix %q", got.Number, wantPrefix)
	}
	if got.VendorPayee != req.Name || got.Vendor != req.Name {
		t.Fatalf("payee = %q/%q, want requester %q (forced)", got.VendorPayee, got.Vendor, req.Name)
	}
	if got.RequesterID != req.ID || got.ManagerID != mgr.ID {
		t.Fatalf("requester/manager = %d/%d, want %d/%d", got.RequesterID, got.ManagerID, req.ID, mgr.ID)
	}
	// T5: the urgent flag and its reason round-trip.
	if !got.Urgent || got.UrgencyReason == "" {
		t.Fatalf("urgent=%v reason=%q; both must round-trip", got.Urgent, got.UrgencyReason)
	}
	// C3: amount is stored/round-tripped as int64 paise and formats via money.FormatPaise.
	if got.Amount != 50000 || money.FormatPaise(got.Amount) != "₹500.00" {
		t.Fatalf("amount = %d / %q, want 50000 / ₹500.00", got.Amount, money.FormatPaise(got.Amount))
	}
	audit, err := s.Audit(ctx, "payment_request", id, 5)
	if err != nil || len(audit) == 0 || audit[0].Action != "submit" {
		t.Fatalf("audit = %#v, %v; want a submit entry on payment_request", audit, err)
	}
}

// D1 proof of absence: coverage row L1 inverted. No draft state exists anywhere.
func TestNoDraftStateExists(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	if requestStatuses["draft"] {
		t.Fatal("draft is in the status enum")
	}
	for _, to := range []string{"pending", "approved", "returned", "rejected", "withdrawn"} {
		if canTransition("draft", to) || canTransition(to, "draft") {
			t.Fatalf("a transition to or from draft exists (%q)", to)
		}
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-X','draft','budget','vendor_invoice',1,'p',?,?)`, req.ID, mgr.ID); err == nil {
		t.Fatal("the schema accepted status='draft'")
	}
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: headID, Amount: 100, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status == "draft" || got.SubmittedAt == nil || got.Number == "" {
		t.Fatalf("a newly created request must be numbered and pending: %+v", got)
	}
}

func TestCreateRequestRejectsInvalidType(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "t", HeadID: headID, Amount: 1, Purpose: "x", ManagerID: mgr.ID,
		VendorID: 1, InvoiceNo: "A/1", InvoiceDate: "2026-07-01"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing project err = %v, want ErrValidation", err)
	}
	// Nothing partial is left behind when validation fails.
	var rows int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("a failed create left %d rows behind", rows)
	}
}

// A staged attachment is written in the same transaction as the request; D1
// left no earlier moment at which a file could be attached.
func TestCreateRequestWritesStagedAttachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Sundaram Electricals Pvt Ltd")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "July switchgear", ProjectID: 1, HeadID: headID, Amount: 100000, Purpose: "panels",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18",
		Attachments: []AttachmentInput{{OriginalName: "inv.pdf", StoredPath: "/tmp/inv.pdf", MimeType: "application/pdf", SizeBytes: 12}},
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	atts, err := s.RequestAttachments(ctx, id)
	if err != nil || len(atts) != 1 || atts[0].OriginalName != "inv.pdf" {
		t.Fatalf("attachments = %#v, %v", atts, err)
	}
	got, _ := s.Request(ctx, id)
	if got.Vendor != "Sundaram Electricals Pvt Ltd" {
		t.Fatalf("vendor name not joined: %q", got.Vendor)
	}
}

// The only surviving use of SubmitRequest: correct and resubmit (returned -> pending).
func TestSubmitRequestResubmitsAReturnedRequest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatal(err)
	}
	// A pending request cannot be "submitted" again — it already is.
	if err := s.SubmitRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("double submit = %v, want ErrValidation", err)
	}
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); err != nil {
		t.Fatalf("resubmit returned: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "pending" || got.SubmittedAt == nil || got.ReminderLastSent != nil {
		t.Fatalf("after resubmit = %+v", got)
	}
}

// T12: retired project/head disappears from new selection but the historical
// request keeps showing its name.
func TestRequestRetainsHistoricalProjectHead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/1", InvoiceDate: "2026-07-01"})
	if err != nil {
		t.Fatal(err)
	}
	// Retire the head.
	if _, err := s.UpsertHead(ctx, headID, 1, "Rent", "5", false, 1); err != nil {
		t.Fatal(err)
	}
	// New selection lists no active heads now...
	active, err := s.ListHeads(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range active {
		if h.ID == headID {
			t.Fatal("retired head still offered for new requests")
		}
	}
	// ...but the historical request still shows the head + project name.
	got, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "Rent" || got.Project != "Operations" {
		t.Fatalf("historical names lost: project=%q head=%q", got.Project, got.Head)
	}
}

func TestUrgencyReasonIsRequiredWhenUrgent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	mk := func(urgent bool, reason string) error {
		_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
			ShortTitle: "Travel", ProjectID: 1, HeadID: headID, Amount: 18400, Purpose: "site visit",
			ExpenseDate: "2026-07-17", ManagerID: mgr.ID, Urgent: urgent, UrgencyReason: reason})
		return err
	}
	// Default mode is "reason" (seeded by migration v3).
	if err := mk(true, "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("urgent without a reason = %v, want ErrValidation", err)
	}
	if err := mk(true, "Personal card bill is due on 29 July"); err != nil {
		t.Fatalf("urgent with a reason: %v", err)
	}
	// Not urgent needs no reason.
	if err := mk(false, ""); err != nil {
		t.Fatalf("non-urgent: %v", err)
	}

	// "free": urgent is allowed with no reason.
	if err := s.SetAppSetting(ctx, mgr, "urgency_mode", "free"); err != nil {
		t.Fatal(err)
	}
	if err := mk(true, ""); err != nil {
		t.Fatalf("urgency_mode=free rejected a reasonless urgent request: %v", err)
	}

	// "disabled": urgent may not be set at all.
	if err := s.SetAppSetting(ctx, mgr, "urgency_mode", "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := mk(true, "still urgent"); !errors.Is(err, ErrValidation) {
		t.Fatalf("urgency_mode=disabled accepted an urgent request = %v, want ErrValidation", err)
	}
	if err := mk(false, ""); err != nil {
		t.Fatalf("urgency_mode=disabled rejected a normal request: %v", err)
	}
}

func TestAttachmentPolicyAsksForAReasonInsteadOfBlocking(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Kaveri Logistics")
	mk := func(in RequestInput) (int64, error) {
		in.Treatment, in.Type = "budget", "vendor_invoice"
		in.ShortTitle, in.ProjectID, in.HeadID = "Freight", 1, headID
		in.Amount, in.Purpose, in.ManagerID = 64500, "freight", mgr.ID
		in.VendorID, in.InvoiceNo, in.InvoiceDate = vendorID, "KL/2026/0788", "2026-07-21"
		return s.CreateRequest(ctx, req, in)
	}
	// Off by default: no file, no reason, accepted.
	if _, err := mk(RequestInput{}); err != nil {
		t.Fatalf("attachments optional: %v", err)
	}
	if err := s.SetAppSetting(ctx, mgr, "require_attachments", "1"); err != nil {
		t.Fatal(err)
	}
	// On + no file + no reason -> rejected.
	if _, err := mk(RequestInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("required attachment missing = %v, want ErrValidation", err)
	}
	// On + no file + a reason -> accepted, and the reason is visible on the request.
	id, err := mk(RequestInput{AttachmentExceptionReason: "Vendor sends the invoice by post; it arrives Monday"})
	if err != nil {
		t.Fatalf("exception reason must unblock the submit, got %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.AttachmentExceptionReason == "" {
		t.Fatal("the exception reason was not stored where the approver can read it")
	}
	// On + a file -> accepted, no reason needed.
	if _, err := mk(RequestInput{Attachments: []AttachmentInput{{OriginalName: "inv.pdf", StoredPath: "/tmp/inv.pdf", MimeType: "application/pdf", SizeBytes: 9}}}); err != nil {
		t.Fatalf("attached file rejected: %v", err)
	}

	// Resubmitting a returned request obeys the same rule against stored files.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); err != nil {
		t.Fatalf("resubmit with a stored exception reason: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned', attachment_exception_reason='' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("resubmit without file or reason = %v, want ErrValidation", err)
	}
}

func TestSelfApprovalIsRejectedAndNeverOffered(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	// The store refuses to route a request to its own requester.
	_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Lunch", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: req.ID})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("self-approval create = %v, want ErrValidation", err)
	}
	// Pure-input form of the same rule.
	if err := validateRequestInput(RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: 2, Amount: 1, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: 9, RequesterID: 9}, recoverableCategoryRules); !errors.Is(err, ErrValidation) {
		t.Fatalf("validateRequestInput self-approval = %v, want ErrValidation", err)
	}

	// The approver list never contains the requester.
	grantApprovalPermission(t, s, ctx, req.ID)
	grantApprovalPermission(t, s, ctx, mgr.ID)
	approvers, err := s.ListApprovers(ctx, req.ID)
	if err != nil {
		t.Fatalf("ListApprovers: %v", err)
	}
	if len(approvers) != 1 || approvers[0].ID != mgr.ID {
		t.Fatalf("approvers = %#v, want only the manager", approvers)
	}
	for _, a := range approvers {
		if a.ID == req.ID {
			t.Fatal("the requester appears in their own approver list")
		}
	}
}

// grantApprovalPermission gives a user a role carrying approval:approve.
func grantApprovalPermission(t *testing.T, s *Store, ctx context.Context, userID int64) {
	t.Helper()
	var roleID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)='manager'`).Scan(&roleID); err != nil {
		t.Fatalf("seeded Manager role missing: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRequestPendingReroutesAndResetsReminder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	newMgrID, err := s.CreateUser(ctx, "mgr2@example.com", "Second Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	base := RequestInput{Treatment: "budget", Type: "vendor_advance", ShortTitle: "Advance",
		ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance", ManagerID: mgr.ID,
		VendorID: vendorID, AdvanceReason: "40% booking"}
	id, err := s.CreateRequest(ctx, req, base)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a reminder having been sent.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET reminder_last_sent=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	// Requester edits while pending, choosing a different approver.
	edited := base
	edited.Amount, edited.Purpose, edited.ManagerID = 2500, "advance revised", newMgrID
	if err := s.UpdateRequest(ctx, req, id, edited); err != nil {
		t.Fatalf("UpdateRequest pending: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Amount != 2500 || got.ManagerID != newMgrID {
		t.Fatalf("edit not applied: amount=%d manager=%d", got.Amount, got.ManagerID)
	}
	if got.ReminderLastSent != nil {
		t.Fatalf("reminder timer not reset: %v", got.ReminderLastSent)
	}
	if got.Status != "pending" {
		t.Fatalf("status changed on edit: %q", got.Status)
	}
	// A returned request is editable too — that is the correction flow.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRequest(ctx, req, id, edited); err != nil {
		t.Fatalf("UpdateRequest returned: %v", err)
	}
}

func TestUpdateRequestRejectedAfterApproval(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	in := RequestInput{Treatment: "budget", Type: "vendor_advance", ShortTitle: "Advance",
		ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance", ManagerID: mgr.ID,
		VendorID: vendorID, AdvanceReason: "booking"}
	id, _ := s.CreateRequest(ctx, req, in)
	// Drive it to approved directly (ApproveRequest arrives in Task 12) to prove edits are then blocked.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='approved' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	in.Amount = 999
	if err := s.UpdateRequest(ctx, req, id, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("editing approved request = %v, want ErrValidation", err)
	}
}

func TestWithdrawRequestFromPending(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance",
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk()
	if err := s.WithdrawRequest(ctx, req, id); err != nil {
		t.Fatalf("WithdrawRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn", got.Status)
	}
	// Withdrawn is terminal.
	if err := s.WithdrawRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("double withdraw = %v, want ErrValidation", err)
	}
	// G1: an approved request cannot be withdrawn — cancellation is a decision
	// the approver makes, not something the requester does alone.
	other := mk()
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='approved' WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	if err := s.WithdrawRequest(ctx, req, other); !errors.Is(err, ErrValidation) {
		t.Fatalf("withdraw approved = %v, want ErrValidation", err)
	}
	// Only the requester may withdraw.
	third := mk()
	if err := s.WithdrawRequest(ctx, mgr, third); !errors.Is(err, ErrForbidden) {
		t.Fatalf("withdraw by a non-requester = %v, want ErrForbidden", err)
	}
}

func TestApproveRequestAdjustsAmountAndIsAssignedOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	otherID, _ := s.CreateUser(ctx, "other@example.com", "Other Manager", "hash", "admin", true)
	other, _ := s.UserByID(ctx, otherID)
	mk := func(amount int64, purpose string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: amount, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/" + purpose, InvoiceDate: "2026-07-18"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk(100000, "inv")

	// A non-assigned manager cannot approve.
	if err := s.ApproveRequest(ctx, other, id, 100000, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-assigned approve = %v, want ErrForbidden", err)
	}
	// Assigned manager approves with an adjusted amount.
	if err := s.ApproveRequest(ctx, mgr, id, 90000, "approved for 90k"); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "approved" || got.ApprovedAmount == nil || *got.ApprovedAmount != 90000 {
		t.Fatalf("approved = %+v", got)
	}
	if got.ApprovedBy == nil || *got.ApprovedBy != mgr.ID || got.ApprovedAt == nil {
		t.Fatalf("approval metadata = %+v", got)
	}
	// Zero/negative approved amount is rejected.
	id2 := mk(5000, "inv2")
	if err := s.ApproveRequest(ctx, mgr, id2, 0, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero approved amount = %v, want ErrValidation", err)
	}
	// G8, defence in depth: even a row that somehow routed to its own requester
	// cannot be self-approved.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET manager_id=requester_id WHERE id=?`, id2); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, req, id2, 5000, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-approval at approve time = %v, want ErrForbidden", err)
	}
}

func TestReturnAndRejectRequireTextAndAreAssignedOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance",
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	rid := mk()
	if err := s.ReturnRequest(ctx, mgr, rid, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("return without comment = %v, want ErrValidation", err)
	}
	if err := s.ReturnRequest(ctx, mgr, rid, "please attach the quote"); err != nil {
		t.Fatalf("ReturnRequest: %v", err)
	}
	got, _ := s.Request(ctx, rid)
	if got.Status != "returned" || got.DecisionReason != "please attach the quote" {
		t.Fatalf("returned = %+v", got)
	}
	// Returned can be resubmitted (returned -> pending), keeping its number.
	if err := s.SubmitRequest(ctx, req, rid); err != nil {
		t.Fatalf("resubmit returned: %v", err)
	}
	after, _ := s.Request(ctx, rid)
	if after.Number != got.Number {
		t.Fatalf("resubmission changed the number: %q -> %q", got.Number, after.Number)
	}

	jid := mk()
	if err := s.RejectRequest(ctx, mgr, jid, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("reject without reason = %v, want ErrValidation", err)
	}
	if err := s.RejectRequest(ctx, mgr, jid, "duplicate of PR-1"); err != nil {
		t.Fatalf("RejectRequest: %v", err)
	}
	got, _ = s.Request(ctx, jid)
	if got.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
	// Rejected is terminal — cannot resubmit.
	if err := s.SubmitRequest(ctx, req, jid); !errors.Is(err, ErrValidation) {
		t.Fatalf("resubmit rejected = %v, want ErrValidation", err)
	}
}

func TestReassignAndReraise(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	admin, _ := s.UserByID(ctx, mgr.ID) // acts as admin reassigner
	newMgrID, _ := s.CreateUser(ctx, "cover@example.com", "Cover Manager", "hash", "admin", true)
	mk := func(managerID, amount int64) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: amount, Purpose: "advance",
			ManagerID: managerID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk(mgr.ID, 1000)

	if err := s.ReassignRequest(ctx, admin, id, newMgrID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign without reason = %v, want ErrValidation", err)
	}
	if err := s.ReassignRequest(ctx, admin, id, newMgrID, "manager on leave"); err != nil {
		t.Fatalf("ReassignRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.ManagerID != newMgrID || got.Status != "pending" {
		t.Fatalf("after reassign manager=%d status=%q", got.ManagerID, got.Status)
	}
	audit, _ := s.Audit(ctx, "payment_request", id, 10)
	var reassigned bool
	for _, a := range audit {
		// Not "reassign": that action belongs to reservation reassignment, and
		// both write payment_request rows, so a shared name makes an approver
		// swap read as somebody taking over the payment.
		reassigned = reassigned || a.Action == "approval_reassign"
	}
	if !reassigned {
		t.Fatal("reassign not recorded in history")
	}

	// D1: re-raising a rejected request creates a new PENDING request, not a draft.
	rid := mk(newMgrID, 4000)
	newMgr, _ := s.UserByID(ctx, newMgrID)
	if err := s.RejectRequest(ctx, newMgr, rid, "out of budget"); err != nil {
		t.Fatal(err)
	}
	copyID, err := s.ReraiseRequest(ctx, req, rid)
	if err != nil {
		t.Fatalf("ReraiseRequest: %v", err)
	}
	if copyID == rid {
		t.Fatal("re-raise did not create a new request")
	}
	orig, _ := s.Request(ctx, rid)
	fresh, _ := s.Request(ctx, copyID)
	if fresh.Status != "pending" {
		t.Fatalf("re-raised status = %q, want pending (D1: no drafts)", fresh.Status)
	}
	if fresh.SubmittedAt == nil {
		t.Fatal("re-raised request was not submitted")
	}
	if fresh.Amount != 4000 || fresh.Number == orig.Number {
		t.Fatalf("fresh copy = %+v (orig number %s)", fresh, orig.Number)
	}
	// Only the requester may re-raise, and only a rejected request.
	if _, err := s.ReraiseRequest(ctx, newMgr, rid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("re-raise by a non-requester = %v, want ErrForbidden", err)
	}
	if _, err := s.ReraiseRequest(ctx, req, copyID); !errors.Is(err, ErrValidation) {
		t.Fatalf("re-raise of a pending request = %v, want ErrValidation", err)
	}
}

func TestCancellationRequestAcceptAndDecline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	mkApproved := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Binding wire order", ProjectID: 1, HeadID: headID, Amount: 47000,
			Purpose: "advance", ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "40% booking"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApproveRequest(ctx, mgr, id, 47000, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// --- Employee asks; payment freezes. ---
	id := mkApproved()
	if err := s.RequestCancellation(ctx, req, id, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("cancellation without a reason = %v, want ErrValidation", err)
	}
	if err := s.RequestCancellation(ctx, mgr, id, "not mine to cancel"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cancellation asked by a non-requester = %v, want ErrForbidden", err)
	}
	if err := s.RequestCancellation(ctx, req, id, "Site cancelled the order"); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "cancellation_requested" || got.CancelReason != "Site cancelled the order" {
		t.Fatalf("after asking = %+v", got)
	}

	// --- Manager declines: it goes back to approved and Accounts may proceed. ---
	if err := s.DecideCancellation(ctx, mgr, id, false, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("decline without a reason = %v, want ErrValidation", err)
	}
	if err := s.DecideCancellation(ctx, req, id, false, "keep it live"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("decision by a non-approver = %v, want ErrForbidden", err)
	}
	if err := s.DecideCancellation(ctx, mgr, id, false, "Vendor already dispatched; we owe them"); err != nil {
		t.Fatalf("DecideCancellation decline: %v", err)
	}
	got, _ = s.Request(ctx, id)
	if got.Status != "approved" {
		t.Fatalf("declined cancellation left status %q, want approved", got.Status)
	}

	// --- Manager accepts: the request is cancelled and closed. ---
	if err := s.RequestCancellation(ctx, req, id, "Order withdrawn by the site"); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideCancellation(ctx, mgr, id, true, "Agreed, no cancellation charge"); err != nil {
		t.Fatalf("DecideCancellation accept: %v", err)
	}
	got, _ = s.Request(ctx, id)
	if got.Status != "cancelled" {
		t.Fatalf("accepted cancellation left status %q, want cancelled", got.Status)
	}
	// Cancelled is terminal.
	if err := s.RequestCancellation(ctx, req, id, "again"); !errors.Is(err, ErrValidation) {
		t.Fatalf("cancellation of a cancelled request = %v, want ErrValidation", err)
	}

	// --- G2: the approver may cancel outright, with a reason. ---
	direct := mkApproved()
	if err := s.CancelRequest(ctx, mgr, direct, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("outright cancel without a reason = %v, want ErrValidation", err)
	}
	if err := s.CancelRequest(ctx, req, direct, "not my call"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outright cancel by the requester = %v, want ErrForbidden", err)
	}
	if err := s.CancelRequest(ctx, mgr, direct, "Budget pulled for the quarter"); err != nil {
		t.Fatalf("CancelRequest: %v", err)
	}
	got, _ = s.Request(ctx, direct)
	if got.Status != "cancelled" || got.CancelReason != "Budget pulled for the quarter" {
		t.Fatalf("outright cancel = %+v", got)
	}

	// Every step is on the audit trail, which is what `.thread` renders.
	audit, _ := s.Audit(ctx, "payment_request", id, 20)
	want := map[string]bool{"cancel_request": false, "cancel_decline": false, "cancel": false}
	for _, a := range audit {
		if _, ok := want[a.Action]; ok {
			want[a.Action] = true
		}
	}
	for action, seen := range want {
		if !seen {
			t.Fatalf("audit action %q missing from the thread", action)
		}
	}
}

func TestRequestCommentsAndAttachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/1", InvoiceDate: "2026-07-18"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AddRequestComment(ctx, req, id, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty comment = %v, want ErrValidation", err)
	}
	if _, err := s.AddRequestComment(ctx, req, id, "here is the context"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestComment(ctx, mgr, id, "thanks, approved shortly"); err != nil {
		t.Fatal(err)
	}
	comments, err := s.RequestComments(ctx, id)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments = %#v, %v", comments, err)
	}
	if comments[0].AuthorName != req.Name || comments[1].AuthorName != mgr.Name {
		t.Fatalf("comment authors = %q,%q", comments[0].AuthorName, comments[1].AuthorName)
	}

	if _, err := s.AddRequestAttachment(ctx, req, id, AttachmentInput{OriginalName: "quote.pdf", StoredPath: "/tmp/quote.pdf", MimeType: "application/pdf", SizeBytes: 12}); err != nil {
		t.Fatal(err)
	}
	atts, err := s.RequestAttachments(ctx, id)
	if err != nil || len(atts) != 1 || atts[0].OriginalName != "quote.pdf" {
		t.Fatalf("attachments = %#v, %v", atts, err)
	}
}

// A19: history and conversation are ONE chronological stream.
func TestRequestThreadMergesEventsCommentsAndFiles(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 96000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1180", InvoiceDate: "2026-07-18"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestComment(ctx, req, id, "Vendor reissued the invoice."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestAttachment(ctx, req, id, AttachmentInput{OriginalName: "SE-26-27-1184.pdf", StoredPath: "/tmp/a.pdf", MimeType: "application/pdf", SizeBytes: 214000}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRequest(ctx, req, id, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 100000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, mgr, id, 100000, "Panel count matches."); err != nil {
		t.Fatal(err)
	}

	thread, err := s.RequestThread(ctx, id)
	if err != nil {
		t.Fatalf("RequestThread: %v", err)
	}
	if len(thread) < 5 {
		t.Fatalf("thread has %d entries, want the submit, comment, attachment, edit and approval", len(thread))
	}
	kinds := map[string]int{}
	for _, e := range thread {
		kinds[e.Kind]++
	}
	for _, k := range []string{"event", "comment", "attachment"} {
		if kinds[k] == 0 {
			t.Fatalf("thread has no %q entries: %#v", k, thread)
		}
	}
	// Chronological, oldest first — the same order `.thread` renders.
	for i := 1; i < len(thread); i++ {
		if thread[i].CreatedAt.Before(thread[i-1].CreatedAt) {
			t.Fatalf("thread is not chronological at %d", i)
		}
	}
	if thread[0].Kind != "event" || thread[0].Action != "submit" {
		t.Fatalf("thread starts with %#v, want the submit event", thread[0])
	}
	// The edit entry carries a field-level diff for `.tl-change`.
	var sawChange bool
	for _, e := range thread {
		if e.Action == "update" && len(e.Changes) > 0 {
			sawChange = true
			for _, c := range e.Changes {
				if c.Field == "amount" && (c.Was == "" || c.Now == "") {
					t.Fatalf("amount change has no before/after: %#v", c)
				}
			}
		}
	}
	if !sawChange {
		t.Fatal("the edit entry carries no field-level changes")
	}
	// Comments carry initials for the `.tl-dot` avatar.
	for _, e := range thread {
		if e.Kind == "comment" && e.Initials == "" {
			t.Fatalf("comment entry without initials: %#v", e)
		}
	}
}

// The freeze is the status itself: a frozen request is not `approved`, so no
// Phase-3 query that filters on `approved` can pick it up.
//
// This is Task 15's second test. It is verbatim, but it lands with Task 17
// because it is written against ListRequests, which Task 17 introduces.
func TestCancellationFreezesTheApprovedState(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
		ShortTitle: "Wire", ProjectID: 1, HeadID: headID, Amount: 47000, Purpose: "advance",
		ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, mgr, id, 47000, ""); err != nil {
		t.Fatal(err)
	}
	payable, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Status: "approved"})
	if len(payable) != 1 {
		t.Fatalf("approved queue = %d, want 1", len(payable))
	}
	if err := s.RequestCancellation(ctx, req, id, "order withdrawn"); err != nil {
		t.Fatal(err)
	}
	payable, _ = s.ListRequests(ctx, RequestListOptions{Scope: "all", Status: "approved"})
	if len(payable) != 0 {
		t.Fatalf("a frozen request is still in the payable queue: %#v", payable)
	}
}

func TestListRequestsByScopeAndCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	otherID, _ := s.CreateUser(ctx, "someoneelse@example.com", "Someone Else", "hash", "data_entry", true)
	other, _ := s.UserByID(ctx, otherID)
	mk := func(actor User, amount int64, purpose string) int64 {
		id, err := s.CreateRequest(ctx, actor, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: amount, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	own := mk(req, 1000, "mine")
	foreign := mk(other, 2000, "theirs")

	ownList, err := s.ListRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID})
	if err != nil || len(ownList) != 1 || ownList[0].ID != own {
		t.Fatalf("own scope = %#v, %v", ownList, err)
	}
	assigned, _ := s.ListRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Status: "pending"})
	if len(assigned) != 2 {
		t.Fatalf("assigned pending count = %d, want 2", len(assigned))
	}
	all, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all"})
	if len(all) != 2 {
		t.Fatalf("all scope = %d, want 2", len(all))
	}
	found, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Query: "theirs"})
	if len(found) != 1 || found[0].ID != foreign {
		t.Fatalf("query filter = %#v", found)
	}
	n, err := s.CountRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Status: "pending"})
	if err != nil || n != 2 {
		t.Fatalf("CountRequests = %d, %v, want 2", n, err)
	}
	// Search matches the vendor name through the join, not just the snapshot.
	byVendor, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Query: "acme"})
	if len(byVendor) != 2 {
		t.Fatalf("vendor-name search = %d, want 2", len(byVendor))
	}
}

// A19: the `.segmented` tabs are SQL buckets, so tab counts and tab contents
// can never disagree.
func TestListRequestsBuckets(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func(purpose string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	pending := mk("pending one")
	returned := mk("returned one")
	rejected := mk("rejected one")
	if err := s.ReturnRequest(ctx, mgr, returned, "fix the invoice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectRequest(ctx, mgr, rejected, "no budget"); err != nil {
		t.Fatal(err)
	}

	open, _ := s.CountRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "open"})
	if open != 2 {
		t.Fatalf("open bucket = %d, want 2 (pending + returned)", open)
	}
	closed, _ := s.CountRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "closed"})
	if closed != 1 {
		t.Fatalf("closed bucket = %d, want 1", closed)
	}
	// "Needs me" as the requester: the returned one is waiting on them.
	mine, _ := s.ListRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "needs-me"})
	if len(mine) != 1 || mine[0].ID != returned {
		t.Fatalf("requester needs-me = %#v, want the returned request", mine)
	}
	// "Needs me" as the approver: the pending one is waiting on them.
	theirs, _ := s.ListRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Bucket: "needs-me"})
	if len(theirs) != 1 || theirs[0].ID != pending {
		t.Fatalf("approver needs-me = %#v, want the pending request", theirs)
	}
	// Toolbar filters.
	byType, _ := s.CountRequests(ctx, RequestListOptions{Scope: "all", Type: "vendor_advance"})
	if byType != 3 {
		t.Fatalf("type filter = %d, want 3", byType)
	}
	byTreatment, _ := s.CountRequests(ctx, RequestListOptions{Scope: "all", Treatment: "recoverable"})
	if byTreatment != 0 {
		t.Fatalf("treatment filter = %d, want 0", byTreatment)
	}
}

func TestSimilarRequestsFindsRecentNearDuplicatesOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	sundaram := seedTestVendor(t, s, ctx, "Sundaram Electricals Pvt Ltd")
	anand := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	mk := func(vendorID, amount int64, invoice string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
			ShortTitle: "Switchgear", ProjectID: 1, HeadID: headID, Amount: amount, Purpose: "panels",
			ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: invoice, InvoiceDate: "2026-07-18"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	recentA := mk(sundaram, 10000000, "SE/26-27/1102")
	recentB := mk(sundaram, 10000000, "SE/26-27/1184")
	stale := mk(sundaram, 10000000, "SE/26-27/0901")
	_ = mk(anand, 10000000, "AS/26-27/0417")  // different vendor
	_ = mk(sundaram, 250000, "SE/26-27/1200") // same vendor, nowhere near the amount
	gone := mk(sundaram, 10000000, "SE/26-27/1300")
	if _, err := s.DB().Exec(`UPDATE payment_requests SET created_at=datetime('now','-31 days') WHERE id=?`, stale); err != nil {
		t.Fatal(err)
	}
	// A withdrawn request is not a duplicate of anything.
	if err := s.WithdrawRequest(ctx, req, gone); err != nil {
		t.Fatal(err)
	}

	got, err := s.SimilarRequests(ctx, SimilarRequestOptions{
		VendorID: sundaram, Amount: 10000000, InvoiceNo: "SE/26-27/1184",
	})
	if err != nil {
		t.Fatalf("SimilarRequests: %v", err)
	}
	ids := map[int64]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids[recentA] || !ids[recentB] {
		t.Fatalf("near-duplicates missed: %#v", got)
	}
	if ids[stale] {
		t.Fatal("a request older than 30 days was reported as a duplicate")
	}
	if ids[gone] {
		t.Fatal("a withdrawn request was reported as a duplicate")
	}
	if len(got) != 2 {
		t.Fatalf("similar = %d, want exactly the two recent Sundaram requests", len(got))
	}
	// ExcludeID keeps an edit from flagging itself.
	got, _ = s.SimilarRequests(ctx, SimilarRequestOptions{VendorID: sundaram, Amount: 10000000, ExcludeID: recentB})
	for _, r := range got {
		if r.ID == recentB {
			t.Fatal("a request was reported as its own duplicate")
		}
	}
	// A free-text payee (reimbursement, employee advance) matches too.
	byPayee, _ := s.SimilarRequests(ctx, SimilarRequestOptions{Payee: "sundaram electricals pvt ltd", Amount: 10000000})
	if len(byPayee) != 2 {
		t.Fatalf("payee-name match = %d, want 2", len(byPayee))
	}
	// Nothing similar returns an empty slice, never an error.
	none, err := s.SimilarRequests(ctx, SimilarRequestOptions{VendorID: anand, Amount: 1})
	if err != nil || len(none) != 0 {
		t.Fatalf("no matches = %#v, %v; want empty, nil", none, err)
	}
}

// ===========================================================================
// The 2026-07-27 QA audit repairs. One test per finding, named by it.
// ===========================================================================

// auditRequest raises a plain, valid reimbursement so a decision test does not
// have to restate the whole body.
func auditRequest(t *testing.T, s *Store, ctx context.Context, req, mgr User, headID, amount int64) int64 {
	t.Helper()
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Site travel", ProjectID: 1, HeadID: headID, Amount: amount,
		Purpose: "site visit", ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	return id
}

// F-C-01. The approved amount is the authority to pay — G13 caps a payment at it
// — so an approver may reduce it and may never raise it. Before this guard one
// person could authorise ₹25,000 against an ₹18,400 request and Accounts paid the
// larger figure in full.
func TestApproveRequestRefusesMoreThanWasRequested(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	over := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	err := s.ApproveRequest(ctx, mgr, over, 2500000, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("approving ₹25,000 against an ₹18,400 request = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), money.FormatPaise(1840000)) {
		t.Fatalf("the refusal must name what was actually requested: %v", err)
	}
	got, _ := s.Request(ctx, over)
	if got.Status != "pending" || got.ApprovedAmount != nil {
		t.Fatalf("a refused approval wrote something: status=%q approved=%v", got.Status, got.ApprovedAmount)
	}

	// One paise over is still over.
	if err := s.ApproveRequest(ctx, mgr, over, 1840001, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("one paise over = %v, want ErrValidation", err)
	}
	// Exactly the requested amount is the ordinary approval.
	if err := s.ApproveRequest(ctx, mgr, over, 1840000, ""); err != nil {
		t.Fatalf("approving the exact amount: %v", err)
	}
	// And less is the adjustment the approve sheet exists for.
	under := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	if err := s.ApproveRequest(ctx, mgr, under, 1200000, "only the flights"); err != nil {
		t.Fatalf("approving less: %v", err)
	}
	cut, _ := s.Request(ctx, under)
	if cut.ApprovedAmount == nil || *cut.ApprovedAmount != 1200000 {
		t.Fatalf("reduced approval = %v, want 1200000", cut.ApprovedAmount)
	}
}

// F-C-03. legalTransitions carries cancellation_requested → approved for
// DecideCancellation(accept=false) alone: that path is gated on approval:cancel,
// demands a written reason and records `cancel_decline`. ApproveRequest borrowed
// the edge through canTransition and so lifted the requester's freeze with none of
// the three, rewriting approved_amount and leaving a stale cancel_reason on an
// approved row that no screen renders.
func TestApproveIsRefusedWhileACancellationIsUndecided(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	id := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	if err := s.ApproveRequest(ctx, mgr, id, 1840000, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestCancellation(ctx, req, id, "The trip is off."); err != nil {
		t.Fatal(err)
	}

	err := s.ApproveRequest(ctx, mgr, id, 999900, "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("approving a frozen request = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "cancellation") {
		t.Fatalf("the refusal must point at the cancellation: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "cancellation_requested" {
		t.Fatalf("status = %q, want cancellation_requested — the freeze must hold", got.Status)
	}
	if got.ApprovedAmount == nil || *got.ApprovedAmount != 1840000 {
		t.Fatalf("approved_amount was rewritten to %v", got.ApprovedAmount)
	}
	if got.CancelReason != "The trip is off." {
		t.Fatalf("cancel_reason = %q; the requester must still be able to read their own ask", got.CancelReason)
	}
	// Declining is the route that returns it to approved, and it records itself.
	if err := s.DecideCancellation(ctx, mgr, id, false, "Pay it as approved."); err != nil {
		t.Fatal(err)
	}
	trail, err := s.Audit(ctx, "payment_request", id, 20)
	if err != nil {
		t.Fatal(err)
	}
	var declines int
	for _, a := range trail {
		if a.Action == "cancel_decline" {
			declines++
		}
	}
	if declines != 1 {
		t.Fatalf("cancel_decline rows = %d, want exactly 1", declines)
	}
}

// F-C-07. Requirement L7 is "On hold (only Accounts lifts)". A cancellation ask
// used to destroy the hold and a declined cancellation restored nothing, so a
// requester and an approver lifted an accountant's block between them and the
// request came back genuinely re-reservable with the question unanswered.
//
// `on_hold=1` still implies `status='approved'`, so the flag is suspended for the
// duration of the freeze; hold_reason carries the question across it, and the
// decline restores the pause.
func TestADeclinedCancellationRestoresAccountsHold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	accID, err := s.CreateUser(ctx, "acct-hold@example.com", "Asha Accounts", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	acc, _ := s.UserByID(ctx, accID)

	id := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	if err := s.ApproveRequest(ctx, mgr, id, 1840000, ""); err != nil {
		t.Fatal(err)
	}
	const question = "Which head should this hit?"
	if err := s.HoldRequest(ctx, acc, id, question); err != nil {
		t.Fatal(err)
	}

	if err := s.RequestCancellation(ctx, req, id, "Might not need it."); err != nil {
		t.Fatal(err)
	}
	frozen, _ := s.Request(ctx, id)
	// The flag is down, because a hold only ever describes an approved request —
	// but the question is not lost.
	if frozen.OnHold {
		t.Fatal("on_hold survived into cancellation_requested; the invariant is on_hold=1 implies approved")
	}
	if frozen.HoldReason != question {
		t.Fatalf("hold_reason = %q, want the accountant's question kept across the freeze", frozen.HoldReason)
	}

	if err := s.DecideCancellation(ctx, mgr, id, false, "Still needed."); err != nil {
		t.Fatal(err)
	}
	back, _ := s.Request(ctx, id)
	if back.Status != "approved" {
		t.Fatalf("status = %q, want approved", back.Status)
	}
	if !back.OnHold || back.HoldReason != question {
		t.Fatalf("the hold was not restored: on_hold=%v reason=%q", back.OnHold, back.HoldReason)
	}
	// Offered is not the same as permitted: ReserveRequest's conditional UPDATE
	// requires on_hold=0, so a refused reservation is the only proof the pause is
	// real rather than merely rendered.
	if err := s.ReserveRequest(ctx, acc, id); !errors.Is(err, ErrRequestOnHold) {
		t.Fatalf("reserving the re-held request = %v, want ErrRequestOnHold", err)
	}
	// Only Accounts lifts it, and then it is payable.
	if err := s.UnholdRequest(ctx, acc, id); err != nil {
		t.Fatalf("Unhold: %v", err)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatalf("reserve after Accounts lifted the hold: %v", err)
	}
}

// The other branch: accepting the cancellation kills the request, and a dead
// request holds nothing — the hold tab would otherwise keep offering "Read reply"
// on a row no reply can change.
func TestAnAcceptedCancellationClearsTheHoldEntirely(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	accID, _ := s.CreateUser(ctx, "acct-accept@example.com", "Asha Accounts", "hash", "admin", true)
	acc, _ := s.UserByID(ctx, accID)

	id := auditRequest(t, s, ctx, req, mgr, headID, 500000)
	if err := s.ApproveRequest(ctx, mgr, id, 500000, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.HoldRequest(ctx, acc, id, "Bank details?"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestCancellation(ctx, req, id, "Cancel it."); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideCancellation(ctx, mgr, id, true, ""); err != nil {
		t.Fatal(err)
	}
	dead, _ := s.Request(ctx, id)
	if dead.Status != "cancelled" || dead.OnHold || dead.HoldReason != "" {
		t.Fatalf("cancelled request still carries a hold: status=%q on_hold=%v reason=%q",
			dead.Status, dead.OnHold, dead.HoldReason)
	}
}

// F-B-10. CreateRequest reads the numbering settings and then writes. In a
// deferred transaction SQLite refuses that upgrade while another writer is active
// and does not consult the busy handler for it, so busy_timeout could not help and
// the loser of two simultaneous submits met a 500 with their form gone. Both must
// now succeed, with consecutive numbers and no duplicate.
func TestConcurrentSubmitsBothSucceed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	other, err := s.CreateUser(ctx, "second-raiser@example.com", "Second Raiser", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.UserByID(ctx, other)

	const racers = 6
	actors := []User{req, second}
	ids := make([]int64, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = s.CreateRequest(ctx, actors[i%len(actors)], RequestInput{
				Treatment: "budget", Type: "reimbursement", ShortTitle: fmt.Sprintf("Race %d", i),
				ProjectID: 1, HeadID: headID, Amount: 1000 + int64(i), Purpose: "race",
				ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d lost its form to %v — every concurrent submit must commit", i, err)
		}
		r, err := s.Request(ctx, ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if seen[r.Number] {
			t.Fatalf("number %q was handed out twice", r.Number)
		}
		seen[r.Number] = true
	}
	if len(seen) != racers {
		t.Fatalf("%d distinct numbers for %d submits", len(seen), racers)
	}
}

// F-C-05. Two sessions of the same approver deciding at once: exactly one commits
// — that always held — and the loser is now told no rather than handed
// SQLITE_BUSY and a 500. Every pairing in the finding is covered: approve vs
// approve, approve vs reject, and accept vs decline a cancellation.
func TestConcurrentDecisionsRefuseTheLoserRatherThanFailing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	// race runs two writers at once and returns their errors in order.
	race := func(a, b func() error) (error, error) {
		var errA, errB error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; errA = a() }()
		go func() { defer wg.Done(); <-start; errB = b() }()
		close(start)
		wg.Wait()
		return errA, errB
	}
	// exactlyOneWon insists on one nil and one refusal a handler can phrase.
	exactlyOneWon := func(label string, errA, errB error) {
		t.Helper()
		wins := 0
		for _, err := range []error{errA, errB} {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrValidation), errors.Is(err, ErrForbidden):
			default:
				t.Fatalf("%s: the loser met %v — a race must be a refusal, never a server error", label, err)
			}
		}
		if wins != 1 {
			t.Fatalf("%s: %d writers committed, want exactly 1 (%v / %v)", label, wins, errA, errB)
		}
	}

	approveRace := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	a, b := race(
		func() error { return s.ApproveRequest(ctx, mgr, approveRace, 1840000, "first") },
		func() error { return s.ApproveRequest(ctx, mgr, approveRace, 1200000, "second") })
	exactlyOneWon("approve vs approve", a, b)
	if got := requestStatus(t, s, ctx, approveRace); got != "approved" {
		t.Fatalf("approve vs approve left status %q", got)
	}

	mixedRace := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	a, b = race(
		func() error { return s.ApproveRequest(ctx, mgr, mixedRace, 1840000, "") },
		func() error { return s.RejectRequest(ctx, mgr, mixedRace, "Race it.") })
	exactlyOneWon("approve vs reject", a, b)
	if got := requestStatus(t, s, ctx, mixedRace); got != "approved" && got != "rejected" {
		t.Fatalf("approve vs reject left status %q", got)
	}

	cancelRace := auditRequest(t, s, ctx, req, mgr, headID, 1840000)
	if err := s.ApproveRequest(ctx, mgr, cancelRace, 1840000, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestCancellation(ctx, req, cancelRace, "Stop it."); err != nil {
		t.Fatal(err)
	}
	a, b = race(
		func() error { return s.DecideCancellation(ctx, mgr, cancelRace, true, "") },
		func() error { return s.DecideCancellation(ctx, mgr, cancelRace, false, "Keep it.") })
	exactlyOneWon("accept vs decline", a, b)
	if got := requestStatus(t, s, ctx, cancelRace); got != "cancelled" && got != "approved" {
		t.Fatalf("accept vs decline left status %q", got)
	}

	// And one audit row per decision, never two.
	for _, id := range []int64{approveRace, mixedRace, cancelRace} {
		trail, err := s.Audit(ctx, "payment_request", id, 50)
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, entry := range trail {
			counts[entry.Action]++
		}
		for _, action := range []string{"approve", "reject", "cancel", "cancel_decline"} {
			if counts[action] > 1 {
				t.Fatalf("request %d recorded %s %d times", id, action, counts[action])
			}
		}
	}
}

// F-B-16. The list capped itself at 200 rows while the tab count beside it had no
// cap, so a queue promised 214 and drew 200 with nothing saying so, and the CSV
// export dropped the same rows. ListRequestsPage reports the total and whether
// anything was left out; RequestsUnlimited carries every row.
func TestListRequestsPageReportsTheTotalAndTheTruncation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	const total = 205
	for i := 0; i < total; i++ {
		auditRequest(t, s, ctx, req, mgr, headID, int64(1000+i))
	}

	opts := RequestPageOptions{RequestListOptions: RequestListOptions{Scope: "all"}}
	capped, err := s.ListRequestsPage(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if capped.Total != total {
		t.Fatalf("Total = %d, want %d", capped.Total, total)
	}
	if len(capped.Requests) != defaultRequestLimit {
		t.Fatalf("rows = %d, want the default cap of %d", len(capped.Requests), defaultRequestLimit)
	}
	if !capped.Truncated {
		t.Fatal("205 rows behind a 200-row page and Truncated is false — this is the silence F-B-16 is about")
	}

	// Page two finishes the list and says it is the end.
	page2 := opts
	page2.Offset = defaultRequestLimit
	rest, err := s.ListRequestsPage(ctx, page2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Requests) != total-defaultRequestLimit {
		t.Fatalf("page two rows = %d, want %d", len(rest.Requests), total-defaultRequestLimit)
	}
	if rest.Truncated {
		t.Fatal("the last page must not report itself truncated")
	}

	// The export's shape: every row, and nothing hidden.
	whole := opts
	whole.Limit = RequestsUnlimited
	all, err := s.ListRequestsPage(ctx, whole)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Requests) != total || all.Truncated || all.Limit != 0 {
		t.Fatalf("unlimited page = %d rows, truncated=%v limit=%d; want %d rows, false, 0",
			len(all.Requests), all.Truncated, all.Limit, total)
	}
	// No row appears on both pages and none is missed.
	seen := map[int64]bool{}
	for _, r := range append(append([]Request{}, capped.Requests...), rest.Requests...) {
		if seen[r.ID] {
			t.Fatalf("request %d appears on two pages", r.ID)
		}
		seen[r.ID] = true
	}
	if len(seen) != total {
		t.Fatalf("the two pages cover %d rows, want %d", len(seen), total)
	}
}

// F-B-18. "Sorted by who is holding them up" is created_at ASC. It used to be
// DESC, so the request kept waiting longest was last — or on no page at all once
// the cap bit. CURRENT_TIMESTAMP is second-resolution, so id breaks the tie.
func TestListRequestsPutsTheLongestWaitingFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	oldest := auditRequest(t, s, ctx, req, mgr, headID, 1000)
	middle := auditRequest(t, s, ctx, req, mgr, headID, 2000)
	newest := auditRequest(t, s, ctx, req, mgr, headID, 3000)

	list, err := s.ListRequests(ctx, RequestListOptions{Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	got := []int64{}
	for _, r := range list {
		got = append(got, r.ID)
	}
	want := []int64{oldest, middle, newest}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("order = %v, want oldest first %v", got, want)
		}
	}

	// Urgent still jumps the queue, and inside the urgent block the oldest leads.
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET urgent=1 WHERE id IN (?,?)`, middle, newest); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListRequests(ctx, RequestListOptions{Scope: "all"})
	if len(list) != 3 || list[0].ID != middle || list[1].ID != newest || list[2].ID != oldest {
		t.Fatalf("urgent-first order = %v", list)
	}
}

// F-A-08. The approver <select> is built from ListApprovers, so it only ever
// offers people who can approve — and that was the only enforcement. A request
// routed anywhere else can be decided by nobody: the named person fails the route
// gate and every real manager fails the ownership check.
func TestARequestCannotBeRoutedToSomebodyWhoCannotApprove(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	strangerID, err := s.CreateUser(ctx, "nobody@example.com", "No Body", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	// The control: this person is not in the list the form is built from.
	offered, err := s.ListApprovers(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range offered {
		if u.ID == strangerID {
			t.Fatal("the fixture is wrong: the stranger holds approval:approve")
		}
	}

	body := RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Site travel",
		ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "site visit",
		ExpenseDate: "2026-07-21", ManagerID: strangerID}
	if _, err := s.CreateRequest(ctx, req, body); !errors.Is(err, ErrValidation) {
		t.Fatalf("routing to a non-approver = %v, want ErrValidation", err)
	}

	// The edit path is the same hole one route over.
	id := auditRequest(t, s, ctx, req, mgr, headID, 1000)
	reroute := body
	reroute.ManagerID = strangerID
	if err := s.UpdateRequest(ctx, req, id, reroute); !errors.Is(err, ErrValidation) {
		t.Fatalf("rerouting to a non-approver = %v, want ErrValidation", err)
	}
	// So is reassignment, which is this finding's own recovery path.
	if err := s.ReassignRequest(ctx, mgr, id, strangerID, "Take this over."); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassigning to a non-approver = %v, want ErrValidation", err)
	}
	still, _ := s.Request(ctx, id)
	if still.ManagerID != mgr.ID {
		t.Fatalf("a refused reroute moved the approver to %d", still.ManagerID)
	}

	// Granting the verb makes the same person acceptable, which proves the check
	// reads the grant and not something incidental about the account.
	grantApprovalPermission(t, s, ctx, strangerID)
	if _, err := s.CreateRequest(ctx, req, body); err != nil {
		t.Fatalf("routing to a real approver: %v", err)
	}
	// And a deactivated approver is refused too: they strand the request just as
	// completely as somebody holding no verb.
	if _, err := s.DB().ExecContext(ctx, `UPDATE users SET active=0 WHERE id=?`, strangerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRequest(ctx, req, body); !errors.Is(err, ErrValidation) {
		t.Fatalf("routing to a deactivated approver = %v, want ErrValidation", err)
	}
}

// F-B-05, F-B-03, F-B-07. The form narrows all three of these controls, and
// narrowing a <select> is not validation.
func TestCreateRequestRechecksTheIdsTheFormNarrows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")

	otherProject, err := s.UpsertProject(ctx, 0, "People", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	foreignHead, err := s.UpsertHead(ctx, 0, otherProject, "Payroll", "1", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	retiredHead, err := s.UpsertHead(ctx, 0, 1, "Closed cost centre", "1", false, 9)
	if err != nil {
		t.Fatal(err)
	}
	deadVendorID := seedTestVendor(t, s, ctx, "Dead Vendor")
	if _, err := s.DB().ExecContext(ctx, `UPDATE vendors SET status='inactive' WHERE id=?`, deadVendorID); err != nil {
		t.Fatal(err)
	}

	base := RequestInput{Treatment: "budget", Type: "vendor_invoice", ShortTitle: "Switchgear",
		ProjectID: 1, HeadID: headID, VendorID: vendorID, Amount: 100000, Purpose: "panels",
		InvoiceNo: "SE/1", InvoiceDate: "2026-07-18", ManagerID: mgr.ID}
	// The control: the honest body is accepted.
	if _, err := s.CreateRequest(ctx, req, base); err != nil {
		t.Fatalf("the valid body must be accepted: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(in RequestInput) RequestInput
		says   string
	}{
		{"F-B-05 a head under a different project",
			func(in RequestInput) RequestInput { in.HeadID = foreignHead; return in }, "different project"},
		{"F-B-03 a retired head",
			func(in RequestInput) RequestInput { in.HeadID = retiredHead; return in }, "retired"},
		{"F-B-07 an inactive vendor",
			func(in RequestInput) RequestInput { in.VendorID = deadVendorID; return in }, "no longer active"},
	} {
		in := tc.mutate(base)
		_, err := s.CreateRequest(ctx, req, in)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("%s = %v, want ErrValidation", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.says) {
			t.Fatalf("%s: message %q does not say why", tc.name, err)
		}
		// And the same refusal on the edit path.
		id := auditRequest(t, s, ctx, req, mgr, headID, 1000)
		if err := s.UpdateRequest(ctx, req, id, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s on edit = %v, want ErrValidation", tc.name, err)
		}
	}

	// Retiring the *project* takes its heads out of the form too
	// (ListHeads(ctx,true) is `h.active=1 AND p.active=1`), so it must refuse here.
	if _, err := s.UpsertProject(ctx, otherProject, "People", false, 2); err != nil {
		t.Fatal(err)
	}
	closed := base
	closed.ProjectID, closed.HeadID = otherProject, foreignHead
	if _, err := s.CreateRequest(ctx, req, closed); !errors.Is(err, ErrValidation) {
		t.Fatalf("a head under a retired project = %v, want ErrValidation", err)
	}
}

// F-B-15, F-B-04. The form renders these fieldsets as alternatives, so no browser
// can send both — and `hidden` is not validation. A crafted POST stored the
// concealed half and request_detail printed every non-empty column, so a budget
// expense read as recoverable and a reimbursement carried an invoice number.
func TestColumnsAShapeDoesNotOwnAreCleared(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")

	// F-B-15: a budget vendor_invoice carrying the recoverable fieldset.
	budgetID, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Switchgear", ProjectID: 1, HeadID: headID, VendorID: vendorID, Amount: 100000,
		Purpose: "panels", InvoiceNo: "SE/1", InvoiceDate: "2026-07-18", ManagerID: mgr.ID,
		RecoverableCategory: "emd", Counterparty: "Shadow Counterparty",
		ExpectedReturnDate: "2027-03-31", RepaymentNotes: "Terms that do not belong here."})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	budget, _ := s.Request(ctx, budgetID)
	if budget.Counterparty != "" || budget.ExpectedReturnDate != "" || budget.RepaymentNotes != "" {
		t.Fatalf("a budget expense kept recoverable columns: cp=%q return=%q notes=%q",
			budget.Counterparty, budget.ExpectedReturnDate, budget.RepaymentNotes)
	}
	if budget.RecoverableCategory != "" || budget.RecoverableCategoryID != nil {
		t.Fatalf("a budget expense kept a category: %q / %v", budget.RecoverableCategory, budget.RecoverableCategoryID)
	}

	// F-B-04: a reimbursement carrying the invoice and advance fields.
	reimbID, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Cab receipts", ProjectID: 1, HeadID: headID, Amount: 4500, Purpose: "travel",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID, InvoiceNo: "FORGED-1",
		InvoiceDate: "2026-07-01", AdvanceReason: "Not a field this type has."})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	reimb, _ := s.Request(ctx, reimbID)
	if reimb.InvoiceNo != "" || reimb.InvoiceDate != "" || reimb.AdvanceReason != "" {
		t.Fatalf("a reimbursement kept invoice/advance columns: no=%q date=%q reason=%q",
			reimb.InvoiceNo, reimb.InvoiceDate, reimb.AdvanceReason)
	}
	// A forged invoice number must not be able to collide with a genuine one in
	// the duplicate check either. Amount is left at 0 so only the invoice branch
	// of SimilarRequests can match — otherwise the row comes back on its amount
	// and proves nothing.
	dupes, err := s.SimilarRequests(ctx, SimilarRequestOptions{Payee: req.Name, InvoiceNo: "FORGED-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dupes {
		if d.ID == reimbID {
			t.Fatal("the scrubbed invoice number is still matchable")
		}
	}

	// And a vendor_invoice keeps everything it does own.
	if budget.InvoiceNo != "SE/1" || budget.InvoiceDate != "2026-07-18" {
		t.Fatalf("the scrub took a field the type owns: no=%q date=%q", budget.InvoiceNo, budget.InvoiceDate)
	}
	// The edit path scrubs identically.
	if err := s.UpdateRequest(ctx, req, reimbID, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Cab receipts", ProjectID: 1, HeadID: headID, Amount: 4500, Purpose: "travel",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID, InvoiceNo: "FORGED-2",
		Counterparty: "Nobody"}); err != nil {
		t.Fatalf("UpdateRequest: %v", err)
	}
	edited, _ := s.Request(ctx, reimbID)
	if edited.InvoiceNo != "" || edited.Counterparty != "" {
		t.Fatalf("the edit path stored columns the shape does not own: no=%q cp=%q",
			edited.InvoiceNo, edited.Counterparty)
	}
}
