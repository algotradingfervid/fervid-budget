package app

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// Fixtures that put data into the test server's database. Tests seed through
// the store (or, where the store would refuse a shape a test needs, through
// raw SQL) because they are about what happens to the data, not how it got in.

// ---- People and roles ----

// admin is the bootstrap administrator every test server starts with.
func (s *appTestServer) admin() store.User {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	return admin
}

// roleID looks up a seeded role by name, ignoring case.
func (s *appTestServer) roleID(t *testing.T, name string) int64 {
	t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if strings.EqualFold(role.Name, name) {
			return role.ID
		}
	}
	t.Fatalf("no seeded role named %q", name)
	return 0
}

// roleIDByName is roleID as the text the people form posts, so a test can ask
// for "Requester" rather than hard-coding a number the seed decides.
func (s *appTestServer) roleIDByName(t *testing.T, name string) string {
	t.Helper()
	return strconv.FormatInt(s.roleID(t, name), 10)
}

// assignRole makes a seeded role the user's only role, replacing whatever they
// held before.
func (s *appTestServer) assignRole(userID int64, roleName string) {
	s.t.Helper()
	if err := s.st.SetUserRoles(s.ctx, s.admin(), userID, []int64{s.roleID(s.t, roleName)}); err != nil {
		s.t.Fatal(err)
	}
}

// seedUser creates an active user and returns their store record.
//
// legacyRole is the old account-type column ("admin" or "data_entry"). It still
// matters: it decides the default role a new user is given, and the last-admin
// guard counts it. When role is not empty the user ends up holding exactly that
// seeded role; when it is empty they keep the default role.
func (s *appTestServer) seedUser(email, name, password, legacyRole, role string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, legacyRole, true)
	if err != nil {
		s.t.Fatal(err)
	}
	if role != "" {
		s.assignRole(id, role)
	}
	return mustUser(s.t, s, id)
}

// seedRoleUser creates an active data-entry user holding exactly one seeded
// role.
func (s *appTestServer) seedRoleUser(email, name, password, role string) store.User {
	s.t.Helper()
	return s.seedUser(email, name, password, "data_entry", role)
}

// seedRequester creates an active user holding only the Requester role.
func (s *appTestServer) seedRequester(email, name, password string) store.User {
	s.t.Helper()
	return s.seedRoleUser(email, name, password, "Requester")
}

// seedManager creates a user who ends up holding only the Manager role, so
// they may approve and reassign requests.
func (s *appTestServer) seedManager(email, name, password string) store.User {
	s.t.Helper()
	u := s.seedRequester(email, name, password)
	s.assignRole(u.ID, "Manager")
	return u
}

// seedSecondApprover creates Kavita Rao, a Manager who is never the signed-in
// requester, so the rule that nobody approves their own request never gets in
// a fixture's way. It returns her user id.
func seedSecondApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	return s.seedUser("kavita@example.test", "Kavita Rao", "ApproverPass123", "admin", "Manager").ID
}

// seedThirdApprover creates Rakesh Iyer, a second Manager besides Kavita, for
// tests that move a request from one approver to another.
func seedThirdApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	return s.seedUser("rakesh@example.test", "Rakesh Iyer", "ApproverPass456", "admin", "Manager").ID
}

// seedColleague creates a second user who can sign in, so a reservation can be
// held by somebody other than the caller. The legacy "admin" account type gives
// them the default Admin role.
func (s *appTestServer) seedColleague(email, name, password string) store.User {
	s.t.Helper()
	return s.seedUser(email, name, password, "admin", "")
}

// seedProbeUser creates an active user whose single role carries exactly the
// grants and scopes given. The seeded roles are bundles; proving that one cell
// of the permission matrix is enough — or not enough — needs a user holding
// that cell and nothing next to it.
func (s *appTestServer) seedProbeUser(email, name, password, roleName string, grants []store.Grant, scopes []store.ScopeGrant) store.User {
	s.t.Helper()
	admin := s.admin()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, scopes); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
	return mustUser(s.t, s, uid)
}

// seedUserWithGrants is seedProbeUser without scopes, for a user named after
// their role ("<roleName> User"). The seeded roles cannot express, say,
// "vendor:view but not vendor_bank:view"; a one-off role can.
func (s *appTestServer) seedUserWithGrants(email, password, roleName string, grants []store.Grant) {
	s.t.Helper()
	s.seedProbeUser(email, roleName+" User", password, roleName, grants, nil)
}

// grantAlso gives a user one extra custom role carrying the given grants, on
// top of the roles they already hold. The seeded Requester and Manager roles do
// not carry request:cancel or approval:cancel; in the product an administrator
// grants them on the Roles screen, and this does the same thing directly.
func (s *appTestServer) grantAlso(userID int64, roleName string, grants []store.Grant, scopes []store.ScopeGrant) {
	s.t.Helper()
	admin := s.admin()
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, scopes); err != nil {
		s.t.Fatal(err)
	}
	held, err := s.st.UserRoles(s.ctx, userID)
	if err != nil {
		s.t.Fatal(err)
	}
	ids := []int64{roleID}
	for _, role := range held {
		ids = append(ids, role.ID)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, userID, ids); err != nil {
		s.t.Fatal(err)
	}
}

// mustUser reads a user back from the store.
func mustUser(t *testing.T, s *appTestServer, id int64) store.User {
	t.Helper()
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// ---- Budget masters and vendors ----

// seedHead creates project "Operations <name>" with head "Rent <name>" and
// returns the administrator together with the new head's id.
func (s *appTestServer) seedHead(name string) (store.User, int64) {
	s.t.Helper()
	admin := s.admin()
	projectID, err := s.st.UpsertProject(s.ctx, 0, "Operations "+name, true, 1)
	if err != nil {
		s.t.Fatal(err)
	}
	headID, err := s.st.UpsertHead(s.ctx, 0, projectID, "Rent "+name, "5", true, 1)
	if err != nil {
		s.t.Fatal(err)
	}
	return admin, headID
}

// projectOf is the project a head belongs to, for the forms that must re-post it.
func projectOf(t *testing.T, s *appTestServer, headID int64) int64 {
	t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	return projectID
}

// seedVendor creates a vendor as the administrator.
func (s *appTestServer) seedVendor(in store.VendorInput) int64 {
	s.t.Helper()
	id, err := s.st.CreateVendor(s.ctx, s.admin(), in)
	if err != nil {
		s.t.Fatalf("CreateVendor(%s): %v", in.Name, err)
	}
	return id
}

// ---- Requests ----

// sqlExpr is an insertRequest value written into the SQL as-is instead of
// being bound as a parameter.
type sqlExpr string

// sqlNow stores the database's CURRENT_TIMESTAMP.
const sqlNow = sqlExpr("CURRENT_TIMESTAMP")

// requestNumber is the request number fixtures give their seq-th request.
func requestNumber(seq int) string { return fmt.Sprintf("PR-2026-%06d", seq) }

// insertRequest writes one payment_requests row holding exactly the given
// columns; every other column takes its schema default. It returns the row id.
func (s *appTestServer) insertRequest(t *testing.T, cols map[string]any) int64 {
	t.Helper()
	names := slices.Sorted(maps.Keys(cols))
	marks := make([]string, len(names))
	var args []any
	for i, name := range names {
		if expr, ok := cols[name].(sqlExpr); ok {
			marks[i] = string(expr)
			continue
		}
		marks[i] = "?"
		args = append(args, cols[name])
	}
	res, err := s.st.DB().ExecContext(s.ctx,
		"INSERT INTO payment_requests("+strings.Join(names, ",")+") VALUES("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedApprovedRequest inserts an approved, unclaimed vendor-invoice request
// with a typed payee ("Acme Landlord"). Going through the screens would need a
// second user to approve every fixture, and these tests are not about how a
// request gets approved.
func (s *appTestServer) seedApprovedRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	return s.insertRequest(s.t, map[string]any{
		"number": requestNumber(seq), "status": "approved", "treatment": "budget", "type": "vendor_invoice",
		"project_id": projectOf(s.t, s, headID), "head_id": headID, "amount": amount,
		"purpose": "Office rent", "short_title": "Office rent", "vendor_payee": "Acme Landlord",
		"requester_id": requesterID, "manager_id": managerID,
		"approved_amount": amount, "approved_by": managerID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
}

// seedVendorRequest inserts an approved request the way the real form writes a
// vendor invoice: the payee is a vendor_id and the vendor_payee snapshot is
// empty (that column is only filled for reimbursements and employee advances).
// A fixture that writes vendor_payee on a vendor invoice builds a shape the
// screens never produce, and can hide a blank-payee bug.
func (s *appTestServer) seedVendorRequest(seq int, requesterID, managerID, headID, amount int64, vendorName string) (reqID, vendorID int64) {
	s.t.Helper()
	vres, err := s.st.DB().Exec(`INSERT INTO vendors(name,status) VALUES(?,'active')`, vendorName)
	if err != nil {
		s.t.Fatal(err)
	}
	vendorID, _ = vres.LastInsertId()
	reqID = s.insertRequest(s.t, map[string]any{
		"number": requestNumber(seq), "status": "approved", "treatment": "budget", "type": "vendor_invoice",
		"project_id": projectOf(s.t, s, headID), "head_id": headID, "amount": amount,
		"purpose": "Annual licence", "short_title": "Annual licence", "vendor_id": vendorID, "vendor_payee": "",
		"requester_id": requesterID, "manager_id": managerID,
		"approved_amount": amount, "approved_by": managerID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
	return reqID, vendorID
}

// seedPendingRequest is a request awaiting its approver, shaped the way the
// reimbursement form makes one: project and head, an expense date, and a payee
// the store forces to the requester. Reimbursement rather than vendor_invoice
// because it needs no vendor row, so a test that re-posts the whole form does not
// have to seed the vendor master too.
func (s *appTestServer) seedPendingRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	return s.insertRequest(s.t, map[string]any{
		"number": requestNumber(seq), "status": "pending", "treatment": "budget", "type": "reimbursement",
		"project_id": projectOf(s.t, s, headID), "head_id": headID, "amount": amount,
		"purpose": "Site visit", "short_title": "Site visit", "expense_date": "2026-07-01",
		"vendor_payee": "Requester Payee", "requester_id": requesterID, "manager_id": managerID,
		"submitted_at": sqlNow,
	})
}

// categoryIDByCode is the id of the recoverable category with this code.
func categoryIDByCode(t *testing.T, s *appTestServer, code string) int64 {
	t.Helper()
	var catID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	return catID
}

// seedRecoverableRequest is an approved recoverable in the register. It carries
// both the category code and the category id, as both write paths do; every
// register query joins on the id, so a row without it shows a blank category.
func (s *appTestServer) seedRecoverableRequest(seq int, requesterID, managerID int64, code, counterparty string, amount int64) int64 {
	s.t.Helper()
	return s.insertRequest(s.t, map[string]any{
		"number": requestNumber(seq), "status": "approved", "treatment": "recoverable", "type": "recoverable",
		"recoverable_category": code, "recoverable_category_id": categoryIDByCode(s.t, s, code),
		"amount": amount, "purpose": "Inter-corporate deposit", "short_title": "ICD",
		"counterparty": counterparty, "expected_return_date": "2027-03-31", "repayment_notes": "Refundable on maturity",
		"requester_id": requesterID, "manager_id": managerID,
		"approved_amount": amount, "approved_by": managerID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
}

// seedHeadlessRecoverable is an approved EMD recoverable shaped the way the
// request form leaves one: linked to a project, with no head.
func (s *appTestServer) seedHeadlessRecoverable(seq int, requesterID, managerID, projectID, amount int64) int64 {
	s.t.Helper()
	return s.insertRequest(s.t, map[string]any{
		"number": requestNumber(seq), "status": "approved", "treatment": "recoverable", "type": "employee_advance",
		"recoverable_category": "emd", "recoverable_category_id": sqlExpr("(SELECT id FROM recoverable_categories WHERE code='emd')"),
		"project_id": projectID, "amount": amount, "purpose": "Tender deposit", "short_title": "EMD",
		"counterparty": "Ridge Metro", "expected_return_date": "2027-03-31", "repayment_notes": "Refundable on award",
		"requester_id": requesterID, "manager_id": managerID,
		"approved_amount": amount, "approved_by": managerID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
}

// seedRecoverable inserts a completed recoverable request charged to headID
// and, when paidOn is given, its settled payment. It writes both the category
// code and the category id — the shape CreateRequest produces — so the
// register's joins are exercised the way the real form drives them.
func seedRecoverable(t *testing.T, s *appTestServer, headID int64, number, code, counterparty, expectedReturn, paidOn string, amount int64) int64 {
	t.Helper()
	admin := s.admin()
	reqID := s.insertRequest(t, map[string]any{
		"number": number, "status": "completed", "treatment": "recoverable", "type": "recoverable",
		"recoverable_category": code, "recoverable_category_id": categoryIDByCode(t, s, code),
		"head_id": headID, "amount": amount, "purpose": "Inter-corporate deposit", "counterparty": counterparty,
		"expected_return_date": expectedReturn, "repayment_notes": "Recover on maturity",
		"requester_id": admin.ID, "manager_id": admin.ID,
		"approved_amount": amount, "approved_by": admin.ID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
	if paidOn != "" {
		if _, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
			headID, paidOn, amount, counterparty, admin.ID, reqID); err != nil {
			t.Fatal(err)
		}
	}
	return reqID
}

// requestStatusApp reads a request's status straight from the database.
func requestStatusApp(t *testing.T, s *appTestServer, id int64) string {
	t.Helper()
	var status string
	if err := s.st.DB().QueryRow(`SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// ---- Payments ----

// backdateApproval moves a request's approval back to the start of paidOn when
// it is later than that. The store refuses a payment dated before the request
// was approved, so a fixture that settles on a fixed past date first makes the
// approval happen before it. requestID may be an id or its text form.
func backdateApproval(ctx context.Context, db *sql.DB, requestID any, paidOn string) error {
	date := paidOn + " 00:00:00"
	_, err := db.ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, date, requestID, date)
	return err
}

// historicalSettlement records a payment for a request on a fixed past date,
// back-dating the approval first so the date is allowed. Tests of the
// payment-date rule itself call RecordPaymentForRequest directly instead.
func historicalSettlement(ctx context.Context, s *store.Store, actor store.User, id int64, in store.PaymentInput, settlement, reason string, attachment *store.AttachmentInput) (int64, error) {
	if len(in.PaidOn) == 10 {
		if err := backdateApproval(ctx, s.DB(), id, in.PaidOn); err != nil {
			return 0, err
		}
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}

// settleOneRequest seeds an approved request, reserves it and records one
// payment against it through the screens, as the signed-in accountant. It
// returns the request and the payment it produced.
func (s *appTestServer) settleOneRequest(seq int, headID, amount int64, paid, settlement, paidOn string) (int64, int64) {
	s.t.Helper()
	admin := s.admin()
	reqID := s.seedApprovedRequest(seq, admin.ID, admin.ID, headID, amount)
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {paidOn}, "amount": {paid}, "vendor_payee": {"Acme Landlord"},
		"settlement": {settlement}, "reference_no": {"N221260725004417"},
	}
	if settlement == "partial" {
		form.Set("partial_reason", "balance next month")
	}
	requireStatus(s.t, s.postPayment(form), http.StatusSeeOther)
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		s.t.Fatal(err)
	}
	return reqID, payID
}

// seedPartialReview takes one request to partial_review through the screens:
// reserve it, then record a short payment. The caller must already be signed
// in as somebody who can reserve and settle.
func (s *appTestServer) seedPartialReview(reqID, headID int64, reason string) {
	s.t.Helper()
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"},
		"settlement": {"partial"}, "partial_reason": {reason}, "reference_no": {"N9912"}}
	requireStatus(s.t, s.postPayment(form), http.StatusSeeOther)
}

// seedAttachmentFile writes a small text file into the attachment directory and
// describes it the way an upload would.
func (s *appTestServer) seedAttachmentFile(name, content string) store.AttachmentInput {
	s.t.Helper()
	stored := filepath.Join(s.cfg.AttachmentDir, name)
	if err := os.WriteFile(stored, []byte(content), 0600); err != nil {
		s.t.Fatal(err)
	}
	return store.AttachmentInput{OriginalName: name, StoredPath: stored, MimeType: "text/plain", SizeBytes: int64(len(content))}
}

// seedPaymentAttachment inserts a payment_attachments row directly. The store's
// AddAttachment refuses a payment that settles a request, and those are exactly
// the payments whose bank advice these tests are about.
func (s *appTestServer) seedPaymentAttachment(payID, uploader int64, in store.AttachmentInput) int64 {
	s.t.Helper()
	res, err := s.st.DB().Exec(`INSERT INTO payment_attachments(payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`,
		payID, in.OriginalName, in.StoredPath, in.MimeType, in.SizeBytes, uploader)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// ---- Notifications ----

// seedNotification inserts one in-app notification for a user.
func seedNotification(t *testing.T, s *appTestServer, userID int64, event, kind, title, href string) int64 {
	t.Helper()
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO notifications(user_id,event,kind,title,body,href) VALUES(?,?,?,?,?,?)`,
		userID, event, kind, title, "detail line", href)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}
