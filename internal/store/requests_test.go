package store

import (
	"context"
	"errors"
	"strings"
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
			err := validateRequestInput(c.in)
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
		ExpenseDate: "2026-07-21", ManagerID: 9, RequesterID: 9}); !errors.Is(err, ErrValidation) {
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
