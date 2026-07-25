package store

import (
	"context"
	"database/sql"
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

func vendorSelect(withBank bool) string {
	if withBank {
		return vendorColumns + vendorBankColumns
	}
	return vendorColumns
}

// scanVendor reads one row. withBank must match the projection vendorSelect
// produced, so the scan targets and the column list can never drift apart.
func scanVendor(scanner interface{ Scan(...any) error }, withBank bool) (Vendor, error) {
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
	return scanVendor(row, withBank)
}

// ListVendors is the list screen's query. It applies the same bank gate as
// Vendor: one screen cannot become a side door onto another's secret.
func (s *Store) ListVendors(ctx context.Context, opt VendorListOptions, perms PermissionSet) ([]Vendor, error) {
	withBank := canSeeBank(perms)
	if opt.Limit <= 0 {
		opt.Limit = 300
	}

	var where []string
	var args []any
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

	query := `SELECT ` + vendorSelect(withBank) + ` FROM vendors`
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
		v, err := scanVendor(rows, withBank)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
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
