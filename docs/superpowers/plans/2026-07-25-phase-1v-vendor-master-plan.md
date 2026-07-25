# Phase 1V — Vendor Master Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A vendor master that the request form searches, that carries statutory and contact detail, and that keeps bank details behind a permission so they never appear on an employee's request form.

**Why this phase exists and why it sits here:** the approved design (UI/UX spec §8) specifies a vendor master; the original plan set had only a free-text `vendor_payee` column. It must land **after** Phase 1 (it needs the `vendor_bank` permission to exist) and **before** Phase 2, so `payment_requests` is created once with `vendor_id` rather than being migrated twice.

**Architecture:** One `vendors` table at migration **v2**. Bank columns live on the same row but are only selected by a store method that takes the caller's permission set, so an unprivileged caller cannot receive them even through a handler bug. The request form's combobox is served by an htmx fragment endpoint, not JSON.

**Tech Stack:** Go 1.25, `html/template`, htmx, SQLite, Playwright.

## Global Constraints

- Migration version **v2**. The reserved global sequence is v1 permissions, v2 vendors, v3 requests, v4 payments-linking, v5 recoverable categories, v6 notification settings.
- Bank details are gated by `vendor_bank`{view,edit}. **Gating is on the data, not the template** — the store must not return bank fields to a caller without the permission.
- `vendor_payee TEXT` survives alongside `vendor_id` as the payee snapshot for reimbursement and employee-advance requests, where no vendor row exists, and to preserve historical payments.
- Screens must match `mockups/screens/vendors-list.html` and `mockups/screens/vendor-detail.html` exactly, using the Phase 0 component classes.
- Commits: stage only your own files by explicit path; the tree contains unrelated modified files.
- TDD: red → green → commit, one commit per task.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/store/migrations.go` | modify — append the v2 migration |
| `internal/store/vendors.go` | create — `Vendor` type, CRUD, search, bank gating |
| `internal/store/vendors_test.go` | create |
| `internal/app/vendors.go` | create — handlers |
| `internal/app/templates.go` | modify — `vendors_list`, `vendor_detail`, `vendor_combo_options` |
| `internal/app/app.go` | modify — routes |
| `tests/e2e/vendors.spec.ts` | create |

---

### Task 1: Migration v2 — the vendors table

**Files:** Modify `internal/store/migrations.go`; Test `internal/store/migrations_test.go`

**Interfaces:** Produces the `vendors` table consumed by every later task and by Phase 2's `payment_requests.vendor_id`.

- [ ] **Step 1: Write the failing test**

```go
func TestMigrationV2CreatesVendors(t *testing.T) {
	s := newTestStore(t)
	for _, col := range []string{"id", "name", "display_name", "vendor_type", "status",
		"categories", "gstin", "pan", "msme_udyam", "tds_section", "tds_rate",
		"contact_person", "phone", "email", "address", "city", "state", "state_code",
		"bank_account_name", "bank_account_number", "bank_ifsc", "bank_name",
		"bank_branch", "upi_id", "default_payment_mode", "payment_terms_days",
		"notes", "created_at", "updated_at"} {
		var n int
		if err := s.DB().QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('vendors') WHERE name = ?`, col,
		).Scan(&n); err != nil || n != 1 {
			t.Fatalf("vendors.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	if _, err := s.DB().Exec(
		`INSERT INTO vendors(name, vendor_type, status) VALUES('Acme','company','active')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO vendors(name, vendor_type, status) VALUES('acme','company','active')`); err == nil {
		t.Fatal("duplicate vendor name accepted; case-insensitive unique index missing")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/store -run TestMigrationV2 -v` — expect FAIL, column `name` missing.
- [ ] **Step 3: Implement** — append to the `migrations` slice:

```go
{Version: 2, Name: "vendors", Up: func(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS vendors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  vendor_type TEXT NOT NULL DEFAULT 'company',   -- company | proprietor | individual
  status TEXT NOT NULL DEFAULT 'active',         -- active | inactive
  categories TEXT NOT NULL DEFAULT '',
  gstin TEXT NOT NULL DEFAULT '',
  pan TEXT NOT NULL DEFAULT '',
  msme_udyam TEXT NOT NULL DEFAULT '',
  tds_section TEXT NOT NULL DEFAULT '',
  tds_rate TEXT NOT NULL DEFAULT '',
  contact_person TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL DEFAULT '',
  city TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  state_code TEXT NOT NULL DEFAULT '',
  bank_account_name TEXT NOT NULL DEFAULT '',
  bank_account_number TEXT NOT NULL DEFAULT '',
  bank_ifsc TEXT NOT NULL DEFAULT '',
  bank_name TEXT NOT NULL DEFAULT '',
  bank_branch TEXT NOT NULL DEFAULT '',
  upi_id TEXT NOT NULL DEFAULT '',
  default_payment_mode TEXT NOT NULL DEFAULT '',
  payment_terms_days INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vendors_name_nocase ON vendors(lower(name));
CREATE INDEX IF NOT EXISTS idx_vendors_status ON vendors(status);`)
	return err
}},
```

- [ ] **Step 4:** Run until green. **Step 5: Commit** `feat(store): add vendors table at migration v2`

---

### Task 2: Vendor type and bank gating

**Files:** Create `internal/store/vendors.go`, `internal/store/vendors_test.go`

**Interfaces:** Produces

```go
type Vendor struct {
	ID int64; Name, DisplayName, VendorType, Status, Categories string
	GSTIN, PAN, MSMEUdyam, TDSSection, TDSRate string
	ContactPerson, Phone, Email, Address, City, State, StateCode string
	Bank *VendorBank            // nil unless the caller holds vendor_bank:view
	Notes string
	CreatedAt, UpdatedAt time.Time
}
type VendorBank struct {
	AccountName, AccountNumber, IFSC, BankName, Branch, UPIID, DefaultPaymentMode string
	PaymentTermsDays int
}
func (s *Store) Vendor(ctx context.Context, id int64, perms PermissionSet) (Vendor, error)
func (s *Store) ListVendors(ctx context.Context, opt VendorListOptions, perms PermissionSet) ([]Vendor, error)
```

The gate is in the store: when `perms.Can("vendor_bank","view")` is false, `Bank` is left `nil` and the bank columns are not even scanned.

- [ ] **Step 1: Write the failing test** — insert a vendor with bank details; fetch with a permission set granting `vendor_bank:view` and assert `Bank != nil` with the right IFSC; fetch with one that does not and assert `Bank == nil`.
- [ ] **Step 2:** Run, confirm fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(store): add Vendor type with permission-gated bank details`

---

### Task 3: Vendor CRUD

**Files:** Modify `internal/store/vendors.go`, `internal/store/vendors_test.go`

**Interfaces:** Produces `CreateVendor(ctx, actor User, in VendorInput) (int64, error)`, `UpdateVendor(ctx, actor User, id int64, in VendorInput, perms PermissionSet) error`.

Rules: `name` required and case-insensitively unique (map the constraint error to `ErrDuplicate` via `classify`); `vendor_type` ∈ {company, proprietor, individual}; `status` ∈ {active, inactive}; GSTIN, when non-empty, must be 15 characters; every mutation writes `recordAuditTx` with `EntityType: "vendor"`. `UpdateVendor` must **ignore** bank fields entirely when the caller lacks `vendor_bank:edit`, rather than erroring — so a permitted user editing the contact block does not wipe the bank block.

- [ ] **Step 1: Write the failing tests** — create; duplicate name → `ErrDuplicate`; blank name → `ErrValidation`; bad GSTIN length → `ErrValidation`; update without `vendor_bank:edit` leaves the stored IFSC unchanged; audit row written.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(store): add vendor create and update with audit`

---

### Task 4: Vendor search for the combobox

**Files:** Modify `internal/store/vendors.go`, `internal/store/vendors_test.go`

**Interfaces:** Produces `SearchVendors(ctx context.Context, q string, limit int) ([]Vendor, error)`.

Matches `name`, `display_name`, `gstin` and `city`, case-insensitively; **active vendors only**; ordered by exact-prefix first then name; `limit` defaults to 10 and is capped at 25. Never returns bank details.

- [ ] **Step 1: Write the failing test** — seed "Sundaram Electricals Pvt Ltd", "Sundaram Switchgear LLP", one inactive "Sundaram Old"; assert `q="sund"` returns exactly the two active ones, prefix-ordered, with `Bank == nil`.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(store): add vendor search for the request-form combobox`

---

### Task 5: Vendors list screen

**Files:** Create `internal/app/vendors.go`; modify `internal/app/templates.go`, `internal/app/app.go`; Test `internal/app/app_integration_test.go`

Route `GET /vendors` behind `RequirePermission("vendor","view")`. Template matches `mockups/screens/vendors-list.html`: `.page-banner` with `.pb-actions`, the missing-GSTIN `.banner.warn`, desktop `.toolbar` and mobile `.m-filters`, and `table.t-cards` with `data-label` on every cell, `.t-lead` + `.t-sub` on the vendor cell, `.pill.good`/`.pill.neutral` status, and a `<tfoot>` total.

- [ ] **Step 1: Write the failing HTTP test** — a user with `vendor:view` gets 200 containing `t-cards` and the seeded vendor name; a user without it gets 403.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(app): add vendors list screen`

---

### Task 6: Vendor detail screen with gated bank block

**Files:** Modify `internal/app/vendors.go`, `internal/app/templates.go`, `internal/app/app.go`; Test `internal/app/app_integration_test.go`

Routes `GET /vendors/{id}`, `GET /vendors/new`, `POST /vendors`, `POST /vendors/{id}`. Template matches `mockups/screens/vendor-detail.html`: `.segmented` tabs, `<fieldset>` per section (Identity, Statutory, Contact, Payment details, Notes), `.form-grid` with `.field.span-*`/`.m-half`, sticky `.action-bar`.

The Payment-details fieldset renders only when `.Perms.Can "vendor_bank" "view"`; otherwise the `.banner.locked` "You do not have permission to see bank details" block renders in its place. The action bar shows Save only with `vendor:edit`, otherwise a single "Back to vendors".

- [ ] **Step 1: Write the failing HTTP test** — with `vendor_bank:view` the body contains `bank_ifsc`; **without** it, the body contains neither `bank_ifsc` nor the stored IFSC value **anywhere** (proving the gate is on the data, not just the markup), and does contain "permission to see bank details".
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(app): add vendor detail screen with gated bank block`

---

### Task 7: Combobox fragment endpoint

**Files:** Modify `internal/app/vendors.go`, `internal/app/templates.go`, `internal/app/app.go`; Test `internal/app/app_integration_test.go`

Route `GET /vendors/search?q=` behind `RequirePermission("vendor","view")`, returning **an HTML fragment**, not JSON — it is consumed by htmx `hx-get` into `.combo-list`. Renders `.co` rows (`.co-main` with `<b>` name and `<small>` GSTIN · city) plus a trailing `.co.co-add` "＋ Add a new vendor" row **only** when the caller holds `vendor:create`.

The fragment must not render the app shell. Phase 0 Task 16 makes `HX-Request` skip shell construction; this endpoint relies on that.

- [ ] **Step 1: Write the failing test** — `GET /vendors/search?q=sund` with `HX-Request: true` returns a body containing `combo-list` and both vendor names but **no** `<aside`; a caller without `vendor:create` gets no `co-add` row.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(app): add vendor search fragment for the combobox`

---

### Task 8: End-to-end and full-suite verification

**Files:** Create `tests/e2e/vendors.spec.ts`

- [ ] **Step 1:** Spec — admin creates a vendor, sees it in the list, opens it, edits the contact person, saves; at 390 px the list renders as cards and the bottom tab bar does not overlap the action bar.
- [ ] **Step 2:** A second spec asserting a non-privileged session cannot see bank details on the detail screen.
- [ ] **Step 3:** Run `make test-all`.
- [ ] **Step 4: Commit** `test(e2e): cover vendor master journeys`

**Acceptance:** `make test-all` green; `mockups/screens/vendors-list.html` and `vendor-detail.html` both have a corresponding rendered route.

---

## Self-review

- **Spec coverage:** UI/UX §8's four field groups map to Task 2's `Vendor`/`VendorBank` and Task 6's fieldsets; the combobox in §6.1 maps to Tasks 4 and 7; the permission gate to Tasks 2, 6 and 7.
- **Type consistency:** `Vendor`, `VendorBank`, `VendorInput`, `VendorListOptions` are defined in Tasks 2-3 and used unchanged in 4-7. `PermissionSet` comes from Phase 0 Task 12 / Phase 1 Task 7.
- **Downstream contract for Phase 2:** `payment_requests` gains `vendor_id INTEGER REFERENCES vendors(id)` and retains `vendor_payee TEXT` as the snapshot. `requestSelect` gains `LEFT JOIN vendors v ON v.id = r.vendor_id` and selects `COALESCE(v.name, r.vendor_payee)` plus `v.gstin`. Phase 2's per-type validation switches from "vendor_payee non-empty" to "vendor_id > 0" for `vendor_invoice` and `vendor_advance` only.
