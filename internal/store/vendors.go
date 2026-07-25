package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The vendor master (adoption-spec D3 / gap G5).
//
// The one rule this file exists to enforce: bank details are gated on the
// DATA, not on the markup. Every read takes the caller's PermissionSet and,
// when it does not hold vendor_bank:view, the bank columns are left out of the
// SELECT list entirely — they are never fetched, never scanned and never
// present on the returned value. A handler that forgets to check, or a
// template that prints a field it should not, therefore still cannot leak
// them: there is nothing in memory to leak.
//
// That is why Bank is a pointer. A zero-valued struct would be
// indistinguishable from "this vendor has no bank details on file"; nil says
// "this caller was not shown them", and the two are different facts.

type Vendor struct {
	ID          int64
	Name        string
	DisplayName string
	VendorType  string // company | proprietor | individual
	Status      string // active | inactive
	Categories  string // comma separated, free text — used for vendor reporting

	GSTIN      string
	PAN        string
	MSMEUdyam  string
	TDSSection string
	TDSRate    string

	ContactPerson string
	Phone         string
	Email         string
	Address       string
	City          string
	State         string
	StateCode     string

	// Bank is nil unless the caller holds vendor_bank:view. See the file
	// comment: nil means "withheld", not "empty".
	Bank *VendorBank

	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time

	// PaidThisYear and OpenRequests are list-screen context; only ListVendors
	// fills them in and they are zero everywhere else.
	PaidThisYear int64
	OpenRequests int
}

// VendorBank is the restricted block. Default payment mode and payment terms
// belong to it because the approved screen puts them inside the same
// restricted fieldset: how a vendor is paid is part of how they are banked.
type VendorBank struct {
	AccountName        string
	AccountNumber      string
	IFSC               string
	BankName           string
	Branch             string
	UPIID              string
	DefaultPaymentMode string
	PaymentTermsDays   int
}

// VendorListOptions mirrors the four filters the approved list screen offers.
// An empty Status means "active", which is what the screen defaults to.
type VendorListOptions struct {
	Query        string
	VendorType   string
	Category     string
	Status       string // "active" (default) | "inactive" | "all"
	MissingGSTIN bool
	Limit        int
}

// VendorInput is one submitted vendor record. Bank is applied only when the
// caller holds vendor_bank:edit; see UpdateVendor.
type VendorInput struct {
	Name        string
	DisplayName string
	VendorType  string
	Status      string
	Categories  string

	GSTIN      string
	PAN        string
	MSMEUdyam  string
	TDSSection string
	TDSRate    string

	ContactPerson string
	Phone         string
	Email         string
	Address       string
	City          string
	State         string
	StateCode     string

	Notes string

	Bank *VendorBank
}

var vendorTypes = []string{"company", "proprietor", "individual"}
var vendorStatuses = []string{"active", "inactive"}

// gstinLength is fixed by the GST regime: 2 state digits, a 10-character PAN,
// an entity code, a fixed 'Z' and a checksum. The store checks the length only
// — it records statutory identifiers, it never computes tax from them.
const gstinLength = 15

// vendorColumns is everything a vendor is apart from its bank block.
const vendorColumns = `id,name,display_name,vendor_type,status,categories,
	gstin,pan,msme_udyam,tds_section,tds_rate,
	contact_person,phone,email,address,city,state,state_code,
	notes,created_at,updated_at`

// vendorBankColumns is appended only for a caller holding vendor_bank:view.
const vendorBankColumns = `,bank_account_name,bank_account_number,bank_ifsc,
	bank_name,bank_branch,upi_id,default_payment_mode,payment_terms_days`

// canSeeBank is the gate. A nil permission set is a caller whose permissions
// could not be resolved; that is a denial, never an absence of policy.
func canSeeBank(perms PermissionSet) bool {
	return perms != nil && perms.Can("vendor_bank", "view")
}

func canEditBank(perms PermissionSet) bool {
	return perms != nil && perms.Can("vendor_bank", "edit")
}

func vendorSelect(withBank bool) string {
	if withBank {
		return vendorColumns + vendorBankColumns
	}
	return vendorColumns
}

// vendorPaidThisYear is the list screen's "Paid this year" column. Until Phase
// 3 gives payments a vendor_id, the payee snapshot is the only link there is,
// so the match is EXACT: a payment counts towards a vendor only when its
// recorded payee is that vendor's name. It therefore under-counts a payment
// whose payee was typed differently, and can never attribute one to the wrong
// vendor — vendor names are uniquely indexed. On a financial screen, missing a
// row is recoverable; crediting the wrong vendor is not.
const vendorPaidThisYear = `,COALESCE((SELECT SUM(py.amount) FROM payments py
	WHERE py.voided_at IS NULL
	  AND lower(trim(COALESCE(py.vendor_payee,''))) = lower(trim(v.name))
	  AND substr(py.paid_on,1,4) = ?), 0)`

// scanVendor reads one row. withBank and withTotals must match the projection
// vendorSelect produced, so the scan targets and the column list can never
// drift apart.
func scanVendor(scanner interface{ Scan(...any) error }, withBank, withTotals bool) (Vendor, error) {
	var v Vendor
	dest := []any{
		&v.ID, &v.Name, &v.DisplayName, &v.VendorType, &v.Status, &v.Categories,
		&v.GSTIN, &v.PAN, &v.MSMEUdyam, &v.TDSSection, &v.TDSRate,
		&v.ContactPerson, &v.Phone, &v.Email, &v.Address, &v.City, &v.State, &v.StateCode,
		&v.Notes, &v.CreatedAt, &v.UpdatedAt,
	}
	var bank VendorBank
	if withBank {
		dest = append(dest,
			&bank.AccountName, &bank.AccountNumber, &bank.IFSC,
			&bank.BankName, &bank.Branch, &bank.UPIID,
			&bank.DefaultPaymentMode, &bank.PaymentTermsDays)
	}
	if withTotals {
		dest = append(dest, &v.PaidThisYear)
	}
	if err := scanner.Scan(dest...); err != nil {
		if err == sql.ErrNoRows {
			return v, ErrNotFound
		}
		return v, err
	}
	if withBank {
		v.Bank = &bank
	}
	return v, nil
}

// Vendor returns one vendor as the caller is entitled to see it.
func (s *Store) Vendor(ctx context.Context, id int64, perms PermissionSet) (Vendor, error) {
	withBank := canSeeBank(perms)
	row := s.db.QueryRowContext(ctx, `SELECT `+vendorSelect(withBank)+` FROM vendors WHERE id=?`, id)
	return scanVendor(row, withBank, false)
}

// ListVendors is the list screen's query. It applies the same bank gate as
// Vendor: one screen cannot become a side door onto another's secret.
func (s *Store) ListVendors(ctx context.Context, opt VendorListOptions, perms PermissionSet) ([]Vendor, error) {
	withBank := canSeeBank(perms)
	if opt.Limit <= 0 {
		opt.Limit = 300
	}

	// The paid-this-year subquery is in the projection, so its parameter binds
	// before any WHERE parameter.
	args := []any{time.Now().Format("2006")}
	var where []string
	switch strings.ToLower(strings.TrimSpace(opt.Status)) {
	case "all":
	case "inactive":
		where = append(where, `status='inactive'`)
	default:
		where = append(where, `status='active'`)
	}
	if t := strings.ToLower(strings.TrimSpace(opt.VendorType)); t != "" && t != "all" {
		where = append(where, `lower(vendor_type)=?`)
		args = append(args, t)
	}
	if c := strings.ToLower(strings.TrimSpace(opt.Category)); c != "" && c != "all" {
		where = append(where, `instr(lower(categories), ?) > 0`)
		args = append(args, c)
	}
	if q := escapeLike(opt.Query); q != "" {
		where = append(where, `(lower(name) LIKE ? ESCAPE '\' OR lower(display_name) LIKE ? ESCAPE '\'
			OR lower(gstin) LIKE ? ESCAPE '\' OR lower(pan) LIKE ? ESCAPE '\' OR lower(city) LIKE ? ESCAPE '\')`)
		needle := "%" + q + "%"
		args = append(args, needle, needle, needle, needle, needle)
	}
	if opt.MissingGSTIN {
		where = append(where, `trim(gstin)=''`)
	}

	query := `SELECT ` + vendorSelect(withBank) + vendorPaidThisYear + ` FROM vendors v`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY name COLLATE NOCASE LIMIT ?`
	args = append(args, opt.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vendor
	for rows.Next() {
		v, err := scanVendor(rows, withBank, true)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VendorStats is the header line and the missing-GSTIN banner on the list
// screen. It is deliberately independent of the active filters: "3 missing a
// GSTIN" is a fact about the master, not about the current search.
type VendorStats struct {
	Total        int
	Active       int
	Inactive     int
	MissingGSTIN int
}

func (s *Store) VendorStats(ctx context.Context) (VendorStats, error) {
	var out VendorStats
	err := s.db.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(status='active'),0),
		COALESCE(SUM(status='inactive'),0),
		COALESCE(SUM(status='active' AND trim(gstin)=''),0)
		FROM vendors`).Scan(&out.Total, &out.Active, &out.Inactive, &out.MissingGSTIN)
	return out, err
}

// VendorCategories is the list screen's category filter. Categories are a
// comma-separated free-text field on the vendor, so the option list is derived
// from what is actually in use rather than from a fixed enum nobody maintains.
func (s *Store) VendorCategories(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT categories FROM vendors WHERE trim(categories) <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, token := range SplitCategories(raw) {
			seen[strings.ToLower(token)] = token
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for _, token := range seen {
		out = append(out, token)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

// SplitCategories turns the stored comma-separated field into its tokens. It
// is exported because the list screen renders the same tokens as a "·" chain
// and both readings must agree on where the boundaries are.
func SplitCategories(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if token := strings.TrimSpace(part); token != "" {
			out = append(out, token)
		}
	}
	return out
}

// SearchVendors backs the request form's combobox. It takes no PermissionSet
// because it never has one to apply: a picker has no business carrying bank
// details, so it is hard-wired to the bank-free projection. Only active
// vendors are offered — an inactive vendor stays readable on the requests that
// already name it, but must not be pickable for new work.
//
// Ordering puts exact prefix matches first, because someone who has typed
// "sund" is looking for a vendor whose name starts that way, not one that
// merely contains it.
func (s *Store) SearchVendors(ctx context.Context, q string, limit int) ([]Vendor, error) {
	needle := escapeLike(q)
	if needle == "" {
		return nil, nil
	}
	switch {
	case limit <= 0:
		limit = 10
	case limit > 25:
		limit = 25
	}
	prefix := needle + "%"
	contains := "%" + needle + "%"

	rows, err := s.db.QueryContext(ctx, `SELECT `+vendorColumns+` FROM vendors
		WHERE status='active' AND (
			lower(name) LIKE ? ESCAPE '\' OR lower(display_name) LIKE ? ESCAPE '\'
			OR lower(gstin) LIKE ? ESCAPE '\' OR lower(city) LIKE ? ESCAPE '\')
		ORDER BY (CASE WHEN lower(name) LIKE ? ESCAPE '\' OR lower(display_name) LIKE ? ESCAPE '\'
			THEN 0 ELSE 1 END), name COLLATE NOCASE
		LIMIT ?`,
		contains, contains, contains, contains, prefix, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vendor
	for rows.Next() {
		v, err := scanVendor(rows, false, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// normalizeVendorInput trims every field and applies the two defaults the
// approved form itself defaults to, then validates. Returning a cleaned copy
// keeps the caller's value untouched.
func normalizeVendorInput(in VendorInput) (VendorInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.VendorType = strings.ToLower(strings.TrimSpace(in.VendorType))
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	in.Categories = strings.TrimSpace(in.Categories)
	in.GSTIN = strings.ToUpper(strings.TrimSpace(in.GSTIN))
	in.PAN = strings.ToUpper(strings.TrimSpace(in.PAN))
	in.MSMEUdyam = strings.TrimSpace(in.MSMEUdyam)
	in.TDSSection = strings.TrimSpace(in.TDSSection)
	in.TDSRate = strings.TrimSpace(in.TDSRate)
	in.ContactPerson = strings.TrimSpace(in.ContactPerson)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Email = strings.TrimSpace(in.Email)
	in.Address = strings.TrimSpace(in.Address)
	in.City = strings.TrimSpace(in.City)
	in.State = strings.TrimSpace(in.State)
	in.StateCode = strings.TrimSpace(in.StateCode)
	in.Notes = strings.TrimSpace(in.Notes)

	if in.VendorType == "" {
		in.VendorType = "company"
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Name == "" {
		return in, wrapValidation("a vendor name is required")
	}
	if !oneOf(in.VendorType, vendorTypes) {
		return in, wrapValidation("vendor type must be company, proprietor or individual")
	}
	if !oneOf(in.Status, vendorStatuses) {
		return in, wrapValidation("vendor status must be active or inactive")
	}
	if in.GSTIN != "" && len(in.GSTIN) != gstinLength {
		return in, wrapValidation("a GSTIN is 15 characters; leave it blank if the vendor has none")
	}
	return in, nil
}

func wrapValidation(msg string) error {
	return fmt.Errorf("%w: %s", ErrValidation, msg)
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// CreateVendor inserts a vendor. The case-insensitive unique index on name is
// what actually rejects a duplicate — checking first and inserting after is a
// race, so the constraint decides and classify turns it into ErrDuplicate.
func (s *Store) CreateVendor(ctx context.Context, actor User, in VendorInput) (int64, error) {
	in, err := normalizeVendorInput(in)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO vendors(
		name,display_name,vendor_type,status,categories,
		gstin,pan,msme_udyam,tds_section,tds_rate,
		contact_person,phone,email,address,city,state,state_code,notes)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.DisplayName, in.VendorType, in.Status, in.Categories,
		in.GSTIN, in.PAN, in.MSMEUdyam, in.TDSSection, in.TDSRate,
		in.ContactPerson, in.Phone, in.Email, in.Address, in.City, in.State, in.StateCode, in.Notes)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if in.Bank != nil {
		if err := writeVendorBankTx(ctx, tx, id, in.Bank); err != nil {
			return 0, classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{
		ActorID: &actor.ID, ActorName: actor.Name, Action: "create",
		EntityType: "vendor", EntityID: &id,
		Summary: "Created vendor " + in.Name,
		After:   vendorAuditPayload(in, in.Bank != nil),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateVendor rewrites a vendor. The bank block is applied only when the
// caller holds vendor_bank:edit, and is otherwise IGNORED rather than
// rejected: an Accounts clerk without the bank permission must be able to fix
// a phone number without wiping — or being blocked by — a block they cannot
// even see. Refusing the whole save would make the contact fields unusable to
// exactly the people who maintain them.
func (s *Store) UpdateVendor(ctx context.Context, actor User, id int64, in VendorInput, perms PermissionSet) error {
	in, err := normalizeVendorInput(in)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var before string
	err = tx.QueryRowContext(ctx, `SELECT name FROM vendors WHERE id=?`, id).Scan(&before)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE vendors SET
		name=?,display_name=?,vendor_type=?,status=?,categories=?,
		gstin=?,pan=?,msme_udyam=?,tds_section=?,tds_rate=?,
		contact_person=?,phone=?,email=?,address=?,city=?,state=?,state_code=?,notes=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		in.Name, in.DisplayName, in.VendorType, in.Status, in.Categories,
		in.GSTIN, in.PAN, in.MSMEUdyam, in.TDSSection, in.TDSRate,
		in.ContactPerson, in.Phone, in.Email, in.Address, in.City, in.State, in.StateCode, in.Notes,
		id); err != nil {
		return classify(err)
	}

	bankTouched := in.Bank != nil && canEditBank(perms)
	if bankTouched {
		if err := writeVendorBankTx(ctx, tx, id, in.Bank); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{
		ActorID: &actor.ID, ActorName: actor.Name, Action: "update",
		EntityType: "vendor", EntityID: &id,
		Summary: "Updated vendor " + in.Name,
		Before:  map[string]any{"name": before},
		After:   vendorAuditPayload(in, bankTouched),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func writeVendorBankTx(ctx context.Context, tx *sql.Tx, id int64, bank *VendorBank) error {
	_, err := tx.ExecContext(ctx, `UPDATE vendors SET
		bank_account_name=?,bank_account_number=?,bank_ifsc=?,bank_name=?,bank_branch=?,
		upi_id=?,default_payment_mode=?,payment_terms_days=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		strings.TrimSpace(bank.AccountName), strings.TrimSpace(bank.AccountNumber),
		strings.ToUpper(strings.TrimSpace(bank.IFSC)), strings.TrimSpace(bank.BankName),
		strings.TrimSpace(bank.Branch), strings.TrimSpace(bank.UPIID),
		strings.TrimSpace(bank.DefaultPaymentMode), bank.PaymentTermsDays, id)
	return err
}

// vendorAuditPayload deliberately records that the bank block changed, never
// what it changed to. audit:view and vendor_bank:view are different
// permissions, and an account number in before/after JSON would be a second
// copy of the secret sitting behind the weaker of the two.
func vendorAuditPayload(in VendorInput, bankTouched bool) map[string]any {
	return map[string]any{
		"name":         in.Name,
		"vendor_type":  in.VendorType,
		"status":       in.Status,
		"gstin":        in.GSTIN,
		"city":         in.City,
		"bank_changed": bankTouched,
	}
}

// escapeLike neutralises the wildcards in a user-supplied search term so a
// query for "100%" is a search, not a match-everything. Mirrors ListPayments.
func escapeLike(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ""
	}
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}
