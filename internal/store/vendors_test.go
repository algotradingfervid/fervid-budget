package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testIFSC = "HDFC0000521"
const testAccountNumber = "50200041294471"

// seedVendorWithBank writes a vendor and its bank block straight to SQLite, so
// the gating tests do not depend on CreateVendor's own permission handling.
func seedVendorWithBank(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.DB().Exec(`INSERT INTO vendors(name,display_name,vendor_type,status,gstin,city,
		bank_account_name,bank_account_number,bank_ifsc,bank_name,bank_branch,upi_id,
		default_payment_mode,payment_terms_days)
		VALUES(?,?,'company','active','29AABCS1429B1ZQ','Bengaluru',?,?,?,'HDFC Bank','Peenya','pay@sundaram',
		'bank_transfer',30)`,
		name, name+" Short", name+" Private Limited", testAccountNumber, testIFSC)
	if err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func bankViewer() PermissionSet {
	return NewPermissionSet([]Grant{{"vendor", "view"}, {"vendor_bank", "view"}}, nil)
}

func bankBlind() PermissionSet {
	return NewPermissionSet([]Grant{{"vendor", "view"}, {"vendor", "edit"}}, nil)
}

// The whole point of Phase 1V: the gate is on the data. A caller without
// vendor_bank:view must not receive the bank block from the store at all, so a
// handler bug or a template mistake still cannot leak it.
func TestVendorBankDetailsAreGatedByThePermissionSet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := seedVendorWithBank(t, s, "Sundaram Electricals Pvt Ltd")

	permitted, err := s.Vendor(ctx, id, bankViewer())
	if err != nil {
		t.Fatalf("Vendor(with vendor_bank:view): %v", err)
	}
	if permitted.Bank == nil {
		t.Fatal("caller holding vendor_bank:view did not receive the bank block")
	}
	if permitted.Bank.IFSC != testIFSC {
		t.Fatalf("IFSC = %q, want %q", permitted.Bank.IFSC, testIFSC)
	}
	if permitted.Bank.AccountNumber != testAccountNumber {
		t.Fatalf("account number = %q, want %q", permitted.Bank.AccountNumber, testAccountNumber)
	}
	if permitted.Bank.PaymentTermsDays != 30 {
		t.Fatalf("payment terms = %d, want 30", permitted.Bank.PaymentTermsDays)
	}

	denied, err := s.Vendor(ctx, id, bankBlind())
	if err != nil {
		t.Fatalf("Vendor(without vendor_bank:view): %v", err)
	}
	if denied.Bank != nil {
		t.Fatalf("caller lacking vendor_bank:view received bank details: %+v", denied.Bank)
	}
	// Not merely nil-ed on the way out: nothing on the returned value carries
	// the secret anywhere.
	if rendered := fmt.Sprintf("%+v", denied); strings.Contains(rendered, testIFSC) || strings.Contains(rendered, testAccountNumber) {
		t.Fatalf("bank detail leaked into the returned vendor: %s", rendered)
	}
	// The rest of the record still comes back — the gate is on the bank block,
	// not on the vendor.
	if denied.Name != "Sundaram Electricals Pvt Ltd" || denied.GSTIN != "29AABCS1429B1ZQ" {
		t.Fatalf("non-bank fields were withheld too: %+v", denied)
	}
}

func TestListVendorsAppliesTheSameBankGate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedVendorWithBank(t, s, "Sundaram Electricals Pvt Ltd")
	seedVendorWithBank(t, s, "Meridian Facility Services")

	permitted, err := s.ListVendors(ctx, VendorListOptions{}, bankViewer())
	if err != nil {
		t.Fatalf("ListVendors(with vendor_bank:view): %v", err)
	}
	if len(permitted) != 2 {
		t.Fatalf("ListVendors returned %d rows, want 2", len(permitted))
	}
	for _, v := range permitted {
		if v.Bank == nil || v.Bank.IFSC != testIFSC {
			t.Fatalf("%s lost its bank block for a permitted caller: %+v", v.Name, v.Bank)
		}
	}

	denied, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors(without vendor_bank:view): %v", err)
	}
	if len(denied) != 2 {
		t.Fatalf("ListVendors returned %d rows for an unprivileged caller, want 2", len(denied))
	}
	rendered := fmt.Sprintf("%+v", denied)
	if strings.Contains(rendered, testIFSC) || strings.Contains(rendered, testAccountNumber) {
		t.Fatalf("list leaked bank detail to an unprivileged caller: %s", rendered)
	}
	for _, v := range denied {
		if v.Bank != nil {
			t.Fatalf("%s carried a bank block for an unprivileged caller", v.Name)
		}
	}
}

// A nil permission set is a caller whose permissions could not be resolved. It
// must be treated as deny, never as "no gate configured".
func TestVendorTreatsANilPermissionSetAsDeny(t *testing.T) {
	s := newTestStore(t)
	id := seedVendorWithBank(t, s, "Kaveri Logistics")

	v, err := s.Vendor(context.Background(), id, nil)
	if err != nil {
		t.Fatalf("Vendor(nil perms): %v", err)
	}
	if v.Bank != nil {
		t.Fatal("a nil permission set was treated as permission to see bank details")
	}
}

func TestVendorNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Vendor(context.Background(), 4242, bankViewer()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Vendor(missing) = %v, want ErrNotFound", err)
	}
}

func vendorActor(t *testing.T, s *Store, ctx context.Context) User {
	t.Helper()
	id, err := s.CreateUser(ctx, "vendor-admin@example.com", "Vendor Admin", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
}

func validVendorInput() VendorInput {
	return VendorInput{
		Name:          "Sundaram Electricals Pvt Ltd",
		DisplayName:   "Sundaram Elec",
		VendorType:    "company",
		Status:        "active",
		Categories:    "Materials, Switchgear",
		GSTIN:         "29AABCS1429B1ZQ",
		PAN:           "AABCS1429B",
		ContactPerson: "R. Subramanian",
		City:          "Bengaluru",
	}
}

func TestCreateVendorStoresTheRecordAndWritesAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)

	id, err := s.CreateVendor(ctx, actor, validVendorInput())
	if err != nil {
		t.Fatalf("CreateVendor: %v", err)
	}
	v, err := s.Vendor(ctx, id, bankViewer())
	if err != nil {
		t.Fatalf("Vendor: %v", err)
	}
	if v.Name != "Sundaram Electricals Pvt Ltd" || v.DisplayName != "Sundaram Elec" ||
		v.VendorType != "company" || v.Status != "active" || v.GSTIN != "29AABCS1429B1ZQ" ||
		v.City != "Bengaluru" {
		t.Fatalf("CreateVendor stored %+v", v)
	}

	entries, err := s.Audit(ctx, "vendor", id, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Audit(vendor) = %d entries, %v; want 1", len(entries), err)
	}
	if entries[0].Action != "create" || entries[0].ActorName != actor.Name {
		t.Fatalf("audit entry = %+v", entries[0])
	}
}

func TestCreateVendorDefaultsTypeAndStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)

	in := validVendorInput()
	in.VendorType = ""
	in.Status = ""
	id, err := s.CreateVendor(ctx, actor, in)
	if err != nil {
		t.Fatalf("CreateVendor: %v", err)
	}
	v, _ := s.Vendor(ctx, id, bankBlind())
	if v.VendorType != "company" || v.Status != "active" {
		t.Fatalf("defaults = %q/%q, want company/active", v.VendorType, v.Status)
	}
}

func TestCreateVendorRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)

	blank := validVendorInput()
	blank.Name = "   "
	if _, err := s.CreateVendor(ctx, actor, blank); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name = %v, want ErrValidation", err)
	}

	badGST := validVendorInput()
	badGST.GSTIN = "29AABCS1429"
	if _, err := s.CreateVendor(ctx, actor, badGST); !errors.Is(err, ErrValidation) {
		t.Fatalf("11-character GSTIN = %v, want ErrValidation", err)
	}

	badType := validVendorInput()
	badType.VendorType = "partnership"
	if _, err := s.CreateVendor(ctx, actor, badType); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown vendor type = %v, want ErrValidation", err)
	}

	badStatus := validVendorInput()
	badStatus.Status = "archived"
	if _, err := s.CreateVendor(ctx, actor, badStatus); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown status = %v, want ErrValidation", err)
	}

	if rows, err := s.ListVendors(ctx, VendorListOptions{Status: "all"}, bankBlind()); err != nil || len(rows) != 0 {
		t.Fatalf("a rejected create still wrote %d rows (%v)", len(rows), err)
	}
}

func TestCreateVendorRejectsDuplicateNameCaseInsensitively(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)

	if _, err := s.CreateVendor(ctx, actor, validVendorInput()); err != nil {
		t.Fatal(err)
	}
	dupe := validVendorInput()
	dupe.Name = "sundaram electricals pvt ltd"
	if _, err := s.CreateVendor(ctx, actor, dupe); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name = %v, want ErrDuplicate", err)
	}
}

// The rule that keeps a permitted contact-block edit from wiping the bank
// block: without vendor_bank:edit the bank fields are ignored, not rejected.
func TestUpdateVendorIgnoresBankFieldsWithoutPermission(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)
	id := seedVendorWithBank(t, s, "Sundaram Electricals Pvt Ltd")

	in := validVendorInput()
	in.ContactPerson = "New Person"
	in.Bank = &VendorBank{AccountName: "Attacker", AccountNumber: "0000", IFSC: "EVIL0000001"}

	if err := s.UpdateVendor(ctx, actor, id, in, bankBlind()); err != nil {
		t.Fatalf("UpdateVendor: %v", err)
	}
	after, err := s.Vendor(ctx, id, bankViewer())
	if err != nil {
		t.Fatal(err)
	}
	if after.ContactPerson != "New Person" {
		t.Fatalf("the permitted part of the edit was not applied: %+v", after)
	}
	if after.Bank == nil || after.Bank.IFSC != testIFSC || after.Bank.AccountNumber != testAccountNumber {
		t.Fatalf("an unprivileged edit changed the bank block: %+v", after.Bank)
	}
}

func TestUpdateVendorAppliesBankFieldsWithPermission(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)
	id := seedVendorWithBank(t, s, "Sundaram Electricals Pvt Ltd")

	editor := NewPermissionSet([]Grant{{"vendor", "edit"}, {"vendor_bank", "view"}, {"vendor_bank", "edit"}}, nil)
	in := validVendorInput()
	in.Bank = &VendorBank{
		AccountName:        "Sundaram Electricals Private Limited",
		AccountNumber:      "99999999",
		IFSC:               "ICIC0000042",
		BankName:           "ICICI Bank",
		Branch:             "Peenya",
		UPIID:              "pay@icici",
		DefaultPaymentMode: "rtgs",
		PaymentTermsDays:   45,
	}
	if err := s.UpdateVendor(ctx, actor, id, in, editor); err != nil {
		t.Fatalf("UpdateVendor: %v", err)
	}
	after, _ := s.Vendor(ctx, id, bankViewer())
	if after.Bank == nil || after.Bank.IFSC != "ICIC0000042" || after.Bank.PaymentTermsDays != 45 {
		t.Fatalf("privileged bank edit was not applied: %+v", after.Bank)
	}
}

// The audit log is readable by anyone holding audit:view, which is a different
// permission from vendor_bank:view. A bank value written into before/after JSON
// would be a second, unguarded copy of the secret.
func TestVendorAuditNeverRecordsBankValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)
	id := seedVendorWithBank(t, s, "Sundaram Electricals Pvt Ltd")

	editor := NewPermissionSet([]Grant{{"vendor", "edit"}, {"vendor_bank", "view"}, {"vendor_bank", "edit"}}, nil)
	in := validVendorInput()
	in.Bank = &VendorBank{AccountName: "Sundaram", AccountNumber: "12345678", IFSC: "ICIC0000042"}
	if err := s.UpdateVendor(ctx, actor, id, in, editor); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Audit(ctx, "vendor", id, 10)
	if err != nil || len(entries) == 0 {
		t.Fatalf("Audit(vendor) = %d entries, %v", len(entries), err)
	}
	for _, e := range entries {
		blob := e.Summary + e.BeforeJSON + e.AfterJSON
		for _, secret := range []string{"ICIC0000042", "12345678", testIFSC, testAccountNumber} {
			if strings.Contains(blob, secret) {
				t.Fatalf("audit entry leaked %q: %+v", secret, e)
			}
		}
	}
}

func TestUpdateVendorRejectsMissingRowAndDuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor := vendorActor(t, s, ctx)

	first, err := s.CreateVendor(ctx, actor, validVendorInput())
	if err != nil {
		t.Fatal(err)
	}
	second := validVendorInput()
	second.Name = "Meridian Facility Services"
	secondID, err := s.CreateVendor(ctx, actor, second)
	if err != nil {
		t.Fatal(err)
	}

	clash := validVendorInput()
	clash.Name = "SUNDARAM ELECTRICALS PVT LTD"
	if err := s.UpdateVendor(ctx, actor, secondID, clash, bankBlind()); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename onto an existing name = %v, want ErrDuplicate", err)
	}
	if err := s.UpdateVendor(ctx, actor, 999999, validVendorInput(), bankBlind()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of a missing vendor = %v, want ErrNotFound", err)
	}
	// Renaming a vendor to its own name (different case) is not a clash.
	same := validVendorInput()
	same.Name = "Sundaram Electricals PVT Ltd"
	if err := s.UpdateVendor(ctx, actor, first, same, bankBlind()); err != nil {
		t.Fatalf("re-casing a vendor's own name = %v, want nil", err)
	}
}

func TestListVendorsFiltersAndOrders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status,categories,city,gstin) VALUES
		('Zeta Works','company','active','Materials','Hosur','29AABCS1429B1ZQ'),
		('Anand Steel Traders','proprietor','active','Materials','Bengaluru',''),
		('Perfect Tools','proprietor','inactive','Materials','Bengaluru','')`); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListVendors(ctx, VendorListOptions{Status: "all"}, bankBlind())
	if err != nil || len(all) != 3 {
		t.Fatalf("ListVendors(all) = %d rows, %v; want 3", len(all), err)
	}
	if all[0].Name != "Anand Steel Traders" {
		t.Fatalf("ListVendors is not ordered by name: %+v", all[0].Name)
	}

	active, err := s.ListVendors(ctx, VendorListOptions{Status: "active"}, bankBlind())
	if err != nil || len(active) != 2 {
		t.Fatalf("ListVendors(active) = %d rows, %v; want 2", len(active), err)
	}

	byType, err := s.ListVendors(ctx, VendorListOptions{Status: "all", VendorType: "proprietor"}, bankBlind())
	if err != nil || len(byType) != 2 {
		t.Fatalf("ListVendors(proprietor) = %d rows, %v; want 2", len(byType), err)
	}

	byQuery, err := s.ListVendors(ctx, VendorListOptions{Status: "all", Query: "hosur"}, bankBlind())
	if err != nil || len(byQuery) != 1 || byQuery[0].Name != "Zeta Works" {
		t.Fatalf("ListVendors(q=hosur) = %+v, %v; want [Zeta Works]", byQuery, err)
	}

	missing, err := s.ListVendors(ctx, VendorListOptions{Status: "all", MissingGSTIN: true}, bankBlind())
	if err != nil || len(missing) != 2 {
		t.Fatalf("ListVendors(missing GSTIN) = %d rows, %v; want 2", len(missing), err)
	}
}
