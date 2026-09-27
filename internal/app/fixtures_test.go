package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// roleIDByName resolves a seeded role to the id the people form posts, so a test
// can ask for "Requester" rather than hard-coding a number the seed decides.
func (s *appTestServer) roleIDByName(t *testing.T, name string) string {
	t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if strings.EqualFold(role.Name, name) {
			return strconv.FormatInt(role.ID, 10)
		}
	}
	t.Fatalf("no seeded role named %q", name)
	return ""
}

func (s *appTestServer) seedHead(name string) (store.User, int64) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
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

// seedUserWithGrants creates a signed-in-able user holding exactly the grants
// named — the seeded system roles cannot express "vendor:view but not
// vendor_bank:view", which is the distinction Phase 1V exists to enforce.
func (s *appTestServer) seedUserWithGrants(email, password, roleName string, grants []store.Grant) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, email, roleName+" User", hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, nil); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
}

func (s *appTestServer) seedVendor(in store.VendorInput) int64 {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateVendor(s.ctx, admin, in)
	if err != nil {
		s.t.Fatalf("CreateVendor(%s): %v", in.Name, err)
	}
	return id
}

func (s *appTestServer) seedAttachmentFile(name, content string) store.AttachmentInput {
	s.t.Helper()
	stored := filepath.Join(s.cfg.AttachmentDir, name)
	if err := os.WriteFile(stored, []byte(content), 0600); err != nil {
		s.t.Fatal(err)
	}
	return store.AttachmentInput{OriginalName: name, StoredPath: stored, MimeType: "text/plain", SizeBytes: int64(len(content))}
}

// seedPaymentAttachment inserts a payment_attachments row directly. It has to:
// AddAttachment now refuses a payment that settles a request (F-D-08), which is
// exactly the payment whose bank advice these tests are about.
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

// seedManager is a person holding the Manager role, so they may approve and
// reassign, on top of the Requester grants seedRequester gives.
func (s *appTestServer) seedManager(email, name, password string) store.User {
	s.t.Helper()
	u := s.seedRequester(email, name, password)
	s.assignRole(u.ID, "Manager")
	return u
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

// seedPendingRequest is a request awaiting its approver, shaped the way the
// reimbursement form makes one: project and head, an expense date, and a payee
// the store forces to the requester. Reimbursement rather than vendor_invoice
// because it needs no vendor row, so a test that re-posts the whole form does not
// have to seed the vendor master too.
func (s *appTestServer) seedPendingRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,expense_date,vendor_payee,requester_id,manager_id,submitted_at)
		VALUES(?,'pending','budget','reimbursement',?,?,?,?,?,'2026-07-01',?,?,?,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Site visit", "Site visit",
		"Requester Payee", requesterID, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedRecoverableRequest is an approved recoverable in the register, with its
// category id linked the way both write paths populate it since migration v6 —
// every register query joins on it, so a row without it shows a blank category.
func (s *appTestServer) seedRecoverableRequest(seq int, requesterID, managerID int64, code, counterparty string, amount int64) int64 {
	s.t.Helper()
	var catID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,recoverable_category,recoverable_category_id,amount,purpose,short_title,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','recoverable','recoverable',?,?,?,?,?,?,'2027-03-31',?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), code, catID, amount, "Inter-corporate deposit", "ICD",
		counterparty, "Refundable on maturity", requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func mustUser(t *testing.T, s *appTestServer, id int64) store.User {
	t.Helper()
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// seedHeadlessRecoverable is an approved recoverable shaped the way the request
// form leaves one since v8: linked to a project, with no head.
func (s *appTestServer) seedHeadlessRecoverable(seq int, requesterID, managerID, projectID, amount int64) int64 {
	s.t.Helper()
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,amount,purpose,short_title,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','recoverable','employee_advance','emd',(SELECT id FROM recoverable_categories WHERE code='emd'),?,?,'Tender deposit','EMD','Ridge Metro','2027-03-31','Refundable on award',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, amount, requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// Existing workflow fixtures use fixed historical payment dates. Model an
// approval that really happened before that historical settlement. Security
// date tests call RecordPaymentForRequest directly and never use this helper.
func historicalSettlement(s *store.Store, ctx context.Context, actor store.User, id int64, in store.PaymentInput, settlement, reason string, attachment *store.AttachmentInput) (int64, error) {
	if len(in.PaidOn) == 10 {
		if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, in.PaidOn+" 00:00:00", id, in.PaidOn+" 00:00:00"); err != nil {
			return 0, err
		}
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}

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

// seedApprovedRequest inserts an approved, unclaimed request directly. Going
// through the UI would need a second user for the approval (G8) on every single
// fixture, and none of these tests are about how a request gets approved.
func (s *appTestServer) seedApprovedRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Office rent", "Office rent", "Acme Landlord",
		requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedVendorRequest inserts an approved request the way the *real form* writes
// one: the payee is a vendor_id and the vendor_payee snapshot is empty. That
// column is only filled for reimbursement and employee advance, so every fixture
// that writes vendor_payee directly is the one shape the UI can never produce —
// which is exactly how the blank-payee defect survived a green suite.
func (s *appTestServer) seedVendorRequest(seq int, requesterID, managerID, headID, amount int64, vendorName string) (reqID, vendorID int64) {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	vres, err := s.st.DB().Exec(`INSERT INTO vendors(name,status) VALUES(?,'active')`, vendorName)
	if err != nil {
		s.t.Fatal(err)
	}
	vendorID, _ = vres.LastInsertId()
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,vendor_id,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,'',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Annual licence", "Annual licence", vendorID,
		requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	reqID, _ = res.LastInsertId()
	return reqID, vendorID
}

func requestStatusApp(t *testing.T, s *appTestServer, id int64) string {
	t.Helper()
	var status string
	if err := s.st.DB().QueryRow(`SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// seedColleague creates a second signed-in-able user so a reservation can be
// held by somebody other than the caller.
func (s *appTestServer) seedColleague(email, name, password string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "admin", true)
	if err != nil {
		s.t.Fatal(err)
	}
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// settleOneRequest drives the whole flow once — reserve, then confirm — and
// returns the request and the payment it produced. Half the assertions below
// are about what a settled payment must never offer, and none of them are about
// how it got settled.
func (s *appTestServer) settleOneRequest(seq int, headID, amount int64, paid, settlement, paidOn string) (int64, int64) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
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
	requireStatus(s.t, s.postForm("/payments", form), http.StatusSeeOther)
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		s.t.Fatal(err)
	}
	return reqID, payID
}

// seedPartialReview takes one request all the way to partial_review through the
// product, so every test below starts where the manager starts. The caller must
// already be signed in as somebody who can reserve and settle.
func (s *appTestServer) seedPartialReview(reqID, headID int64, reason string) {
	s.t.Helper()
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"},
		"settlement": {"partial"}, "partial_reason": {reason}, "reference_no": {"N9912"}}
	requireStatus(s.t, s.postForm("/payments", form), http.StatusSeeOther)
}

// seedProbeUser creates an active user whose single role carries exactly the
// grants and scopes given. The seeded roles are bundles; proving that one cell
// of the permission matrix is enough — or not enough — needs a user holding
// that cell and nothing next to it.
func (s *appTestServer) seedProbeUser(email, name, password, roleName string, grants []store.Grant, scopes []store.ScopeGrant) store.User {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
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
	u, err := s.st.UserByID(s.ctx, uid)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// seedRecoverable inserts a recoverable request and, when paidOn is given, its
// settled payment. It writes both the category code and the category id — the
// shape CreateRequest produces — so the register's joins are exercised the way
// the real form drives them.
func seedRecoverable(t *testing.T, s *appTestServer, headID int64, number, code, counterparty, expectedReturn, paidOn string, amount int64) int64 {
	t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	var catID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category,recoverable_category_id,head_id,amount,purpose,counterparty,
		 expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?, 'completed','recoverable','recoverable',?,?,?,?, 'Inter-corporate deposit',?,?, 'Recover on maturity',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		number, code, catID, headID, amount, counterparty, expectedReturn, admin.ID, admin.ID, amount, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := res.LastInsertId()
	if paidOn != "" {
		if _, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
			headID, paidOn, amount, counterparty, admin.ID, reqID); err != nil {
			t.Fatal(err)
		}
	}
	return reqID
}

// assignRole grants a Phase-1 seeded role to a user by name, replacing whatever
// the legacy-role back-fill gave them.
func (s *appTestServer) assignRole(userID int64, roleName string) {
	s.t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	var roleID int64
	for _, r := range roles {
		if strings.EqualFold(r.Name, roleName) {
			roleID = r.ID
		}
	}
	if roleID == 0 {
		s.t.Fatalf("seeded role %q not found", roleName)
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, userID, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
}

// seedRequester creates an active user holding only the Requester role and
// returns their store record.
func (s *appTestServer) seedRequester(email, name, password string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	s.assignRole(id, "Requester")
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// grantAlso adds a second role carrying extra grants to a user who already
// holds one. Phase 1's starter roles were never given request:cancel or
// approval:cancel — both verbs entered the canonical vocabulary with this
// phase's cancellation flow, and nothing back-filled the seeded Requester and
// Manager roles with them. In the product an administrator grants them on the
// Roles screen; the fixture does the same thing directly.
func (s *appTestServer) grantAlso(userID int64, roleName string, grants []store.Grant, scopes []store.ScopeGrant) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
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

// seedSecondApprover returns a user id that holds approval:approve and is never
// the logged-in requester, so G8 cannot get in a fixture's way.
func seedSecondApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ApproverPass123")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, "kavita@example.test", "Kavita Rao", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}

func seedThirdApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ApproverPass456")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, "rakesh@example.test", "Rakesh Iyer", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}

// seedRoleUser creates an active user holding exactly one seeded system role.
func (s *appTestServer) seedRoleUser(email, name, password, role string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	s.assignRole(id, role)
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}
