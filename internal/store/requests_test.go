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
