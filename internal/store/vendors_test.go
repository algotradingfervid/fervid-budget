package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
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
	return NewPermissionSet([]Grant{{"vendor", "view"}, {"vendor", "edit"}, {"payment", "view"}, {"request", "view"}}, []ScopeGrant{{"payment", "all"}, {"request", "all"}})
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

func TestVendorStatsAndCategories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status,gstin,categories) VALUES
		('Sundaram Electricals Pvt Ltd','company','active','29AABCS1429B1ZQ','Materials, Switchgear'),
		('Nova Print Works','company','active','','Printing'),
		('Kaveri Logistics','proprietor','active','','Logistics, Materials'),
		('Perfect Tools','proprietor','inactive','','')`); err != nil {
		t.Fatal(err)
	}

	stats, err := s.VendorStats(ctx)
	if err != nil {
		t.Fatalf("VendorStats: %v", err)
	}
	if stats.Total != 4 || stats.Active != 3 || stats.Inactive != 1 {
		t.Fatalf("VendorStats = %+v; want total 4, active 3, inactive 1", stats)
	}
	// The gap the warning banner reports is about vendors in use, so an
	// inactive vendor without a GSTIN is not a gap worth chasing.
	if stats.MissingGSTIN != 2 {
		t.Fatalf("MissingGSTIN = %d, want 2 (active vendors only)", stats.MissingGSTIN)
	}

	cats, err := s.VendorCategories(ctx)
	if err != nil {
		t.Fatalf("VendorCategories: %v", err)
	}
	if strings.Join(cats, "|") != "Logistics|Materials|Printing|Switchgear" {
		t.Fatalf("VendorCategories = %v; want the distinct tokens, sorted", cats)
	}
}

// The list screen's "Paid this year" column. Until Phase 3 links payments to
// vendor_id, the only link is the payee snapshot, so the match is exact: it
// may under-count a payment whose payee was typed differently, but it can
// never attribute one to the wrong vendor.
func TestListVendorsTotalsPaymentsRecordedAgainstTheVendorName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor, headID := seedActorAndHead(t, s, ctx)

	if _, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status) VALUES
		('Sundaram Electricals Pvt Ltd','company','active'),
		('Meridian Facility Services','company','active')`); err != nil {
		t.Fatal(err)
	}
	pay := func(payee, paidOn string, amount int64) int64 {
		t.Helper()
		id, err := s.CreatePayment(ctx, actor, PaymentInput{
			HeadID: headID, PaidOn: paidOn, Amount: amount,
			VendorPayee: payee, PaymentMode: "bank_transfer",
		})
		if err != nil {
			t.Fatalf("CreatePayment(%s): %v", payee, err)
		}
		return id
	}
	// Every in-year payment is dated today. A literal month would make this test
	// depend on when in the year it is run: F-D-06 refuses a future paid_on, so
	// `year+"-04-15"` is a payment the store rejects outright every January.
	today := time.Now().Format("2006-01-02")
	pay("Sundaram Electricals Pvt Ltd", today, 500000)
	pay("sundaram electricals pvt ltd", today, 250000)        // case-insensitive
	pay("Sundaram Electricals", today, 999999)                // not the same payee
	pay("Sundaram Electricals Pvt Ltd", "2019-04-15", 111111) // a different year
	voided := pay("Sundaram Electricals Pvt Ltd", today, 777777)
	if err := s.VoidPayment(ctx, actor, voided, "duplicate entry"); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors: %v", err)
	}
	byName := map[string]Vendor{}
	for _, v := range rows {
		byName[v.Name] = v
	}
	if got := byName["Sundaram Electricals Pvt Ltd"].PaidThisYear; got != 750000 {
		t.Fatalf("PaidThisYear = %d, want 750000 (voided, prior-year and other-payee rows excluded)", got)
	}
	if got := byName["Meridian Facility Services"].PaidThisYear; got != 0 {
		t.Fatalf("a vendor with no payments has PaidThisYear = %d, want 0", got)
	}
}

func TestSearchVendorsIsActiveOnlyPrefixFirstAndBankFree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`INSERT INTO vendors(name,display_name,vendor_type,status,gstin,city,bank_ifsc,bank_account_number) VALUES
		('Sundaram Electricals Pvt Ltd','Sundaram Elec','company','active','29AABCS1429B1ZQ','Bengaluru',?,?),
		('Sundaram Switchgear LLP','','company','active','','Hosur',?,?),
		('Metro Sundaram Supplies','','company','active','','Chennai','',''),
		('Sundaram Old Works','','company','inactive','','Bengaluru','','')`,
		testIFSC, testAccountNumber, testIFSC, testAccountNumber); err != nil {
		t.Fatal(err)
	}

	got, err := s.SearchVendors(ctx, "sund", 0)
	if err != nil {
		t.Fatalf("SearchVendors: %v", err)
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Name)
		if v.Bank != nil {
			t.Fatalf("%s carried a bank block into the combobox: %+v", v.Name, v.Bank)
		}
	}
	want := []string{"Sundaram Electricals Pvt Ltd", "Sundaram Switchgear LLP", "Metro Sundaram Supplies"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("SearchVendors(sund) = %v, want %v (prefix matches first, then by name)", names, want)
	}
	// The combobox is a picker for new work: an inactive vendor must not be
	// offered even though it matches.
	if rendered := fmt.Sprintf("%v", names); strings.Contains(rendered, "Sundaram Old Works") {
		t.Fatalf("SearchVendors offered an inactive vendor: %v", names)
	}
	// Nothing anywhere on the result carries a bank value.
	if rendered := fmt.Sprintf("%+v", got); strings.Contains(rendered, testIFSC) || strings.Contains(rendered, testAccountNumber) {
		t.Fatalf("search results leaked bank detail: %s", rendered)
	}
}

func TestSearchVendorsMatchesGSTINAndCityAndCapsTheLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status,gstin,city) VALUES
		('Nova Print Works','company','active','29AAECW3311P1ZM','Chennai')`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"29aaecw", "chennai", "nova"} {
		got, err := s.SearchVendors(ctx, q, 0)
		if err != nil || len(got) != 1 {
			t.Fatalf("SearchVendors(%q) = %d rows, %v; want 1", q, len(got), err)
		}
	}
	if got, err := s.SearchVendors(ctx, "   ", 0); err != nil || len(got) != 0 {
		t.Fatalf("SearchVendors(blank) = %d rows, %v; want 0", len(got), err)
	}

	// 40 more matches, so the default and the cap are both observable.
	for i := 0; i < 40; i++ {
		if _, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status) VALUES(?,'company','active')`,
			fmt.Sprintf("Zenith Supplies %02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.SearchVendors(ctx, "zenith", 0); len(got) != 10 {
		t.Fatalf("SearchVendors default limit returned %d rows, want 10", len(got))
	}
	if got, _ := s.SearchVendors(ctx, "zenith", 100); len(got) != 25 {
		t.Fatalf("SearchVendors(limit=100) returned %d rows, want the 25 cap", len(got))
	}
	if got, _ := s.SearchVendors(ctx, "zenith", 3); len(got) != 3 {
		t.Fatalf("SearchVendors(limit=3) returned %d rows, want 3", len(got))
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

// seedVendorRequest inserts a payment_requests row shaped the way the request
// form makes one for a vendor: vendor_id names the payee and vendor_payee is
// left empty. Reading the snapshot on such a row is the trap PROGRESS.md
// records, and it is exactly the trap a test about vendor totals must not fall
// into — a fixture that fills vendor_payee would make the payee-name fallback
// look like the vendor_id join working.
func seedVendorRequest(t *testing.T, s *Store, ctx context.Context, seq int, status string, requesterID, managerID, headID, vendorID, amount int64) int64 {
	t.Helper()
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatalf("head project: %v", err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,project_id,head_id,vendor_id,amount,purpose,invoice_no,
		 requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,?,'budget','vendor_invoice',?,?,?,?,'Switchgear','INV-1',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), status, projectID, headID, vendorID, amount,
		requesterID, managerID, amount, managerID)
	if err != nil {
		t.Fatalf("seed vendor request: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedVendorNamed(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status) VALUES(?,'company','active')`, name)
	if err != nil {
		t.Fatalf("seed vendor %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func vendorByName(t *testing.T, rows []Vendor, name string) Vendor {
	t.Helper()
	for _, v := range rows {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("vendor %q is not in the list: %+v", name, rows)
	return Vendor{}
}

// TestVendorPaidThisYearSurvivesARename is F-G-009, and the rename is the whole
// point.
//
// The figure used to be string equality between payments.vendor_payee — a
// denormalised snapshot of the payee's name at the moment of settlement — and
// vendors.name. So correcting a vendor's legal name, with the payment untouched
// and still sitting in the ledger, silently zeroed its entire spend history.
// Migration v10 gives a payment the vendor id it was made to, and this asserts
// the figure is now a fact about the vendor rather than about their spelling.
func TestVendorPaidThisYearSurvivesARename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	acc, requester, mgrID, headID := seedRequestParty(t, s, ctx)
	today := time.Now().UTC().Format("2006-01-02")

	vendorID := seedVendorNamed(t, s, "Sundaram Electricals Pvt Ltd")
	seedVendorNamed(t, s, "Meridian Facility Services")

	settled := seedVendorRequest(t, s, ctx, 1, "approved", requester.ID, mgrID, headID, vendorID, 910000)
	if err := s.ReserveRequest(ctx, acc, settled); err != nil {
		t.Fatalf("ReserveRequest: %v", err)
	}
	if _, err := historicalSettlement(s, ctx, acc, settled,
		PaymentInput{PaidOn: today, Amount: 910000}, "settled", "", nil); err != nil {
		t.Fatalf("RecordPaymentForRequest: %v", err)
	}

	rows, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors: %v", err)
	}
	if got := vendorByName(t, rows, "Sundaram Electricals Pvt Ltd").PaidThisYear; got != 910000 {
		t.Fatalf("PaidThisYear before the rename = %d, want 910000", got)
	}
	if got := vendorByName(t, rows, "Meridian Facility Services").PaidThisYear; got != 0 {
		t.Fatalf("a vendor with no payments = %d, want 0", got)
	}

	// The decisive step: the same row, a new legal name, the payment untouched.
	if err := s.UpdateVendor(ctx, acc, vendorID, VendorInput{Name: "Sundaram Electricals Private Limited"},
		NewPermissionSet([]Grant{{"vendor", "edit"}}, nil)); err != nil {
		t.Fatalf("UpdateVendor: %v", err)
	}
	rows, err = s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors after rename: %v", err)
	}
	if got := vendorByName(t, rows, "Sundaram Electricals Private Limited").PaidThisYear; got != 910000 {
		t.Fatalf("PaidThisYear after the rename = %d, want 910000 — renaming a vendor must not erase its payment history", got)
	}
	// And the ledger still shows what it always showed: the payee as it was
	// written at settlement. The fix corrects the total, not the record.
	var payee string
	if err := s.DB().QueryRowContext(ctx, `SELECT vendor_payee FROM payments WHERE request_id=?`, settled).Scan(&payee); err != nil {
		t.Fatal(err)
	}
	if payee != "Sundaram Electricals Pvt Ltd" {
		t.Fatalf("the payment's payee snapshot = %q; a rename must not rewrite history", payee)
	}
}

// TestVendorOpenRequestsCountsLiveWork is F-G-010. The column was declared on
// the struct, scanned by nothing and therefore printed Go's zero value beside
// every vendor, live work or not — which is worse than an absent column,
// because it asserts that there is none.
func TestVendorOpenRequestsCountsLiveWork(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	acc, requester, mgrID, headID := seedRequestParty(t, s, ctx)
	today := time.Now().UTC().Format("2006-01-02")

	vendorID := seedVendorNamed(t, s, "Sundaram Electricals Pvt Ltd")
	seedVendorNamed(t, s, "Meridian Facility Services")

	// Two open, one rejected, and one carried all the way to settled — so the
	// count is a filter and not a row count.
	seedVendorRequest(t, s, ctx, 2, "pending", requester.ID, mgrID, headID, vendorID, 40000)
	seedVendorRequest(t, s, ctx, 3, "approved", requester.ID, mgrID, headID, vendorID, 50000)
	seedVendorRequest(t, s, ctx, 4, "rejected", requester.ID, mgrID, headID, vendorID, 60000)
	settled := seedVendorRequest(t, s, ctx, 5, "approved", requester.ID, mgrID, headID, vendorID, 910000)
	if err := s.ReserveRequest(ctx, acc, settled); err != nil {
		t.Fatalf("ReserveRequest: %v", err)
	}
	if _, err := historicalSettlement(s, ctx, acc, settled,
		PaymentInput{PaidOn: today, Amount: 910000}, "settled", "", nil); err != nil {
		t.Fatalf("RecordPaymentForRequest: %v", err)
	}

	rows, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors: %v", err)
	}
	if got := vendorByName(t, rows, "Sundaram Electricals Pvt Ltd").OpenRequests; got != 2 {
		t.Fatalf("OpenRequests = %d, want 2 (pending + approved; the rejected and settled ones are not open)", got)
	}
	if got := vendorByName(t, rows, "Meridian Facility Services").OpenRequests; got != 0 {
		t.Fatalf("a vendor with no requests = %d, want 0", got)
	}
}

// TestVendorOpenRequestsUsesTheProductsOwnOpenBucket pins the definition rather
// than the number: "open" here is requestBuckets["open"], the set behind
// /requests?bucket=open, so this count and that list cannot drift apart the way
// F-G-006's dashboard tile drifted from the bucket its own link pointed at.
func TestVendorOpenRequestsUsesTheProductsOwnOpenBucket(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, requester, mgrID, headID := seedRequestParty(t, s, ctx)
	vendorID := seedVendorNamed(t, s, "Harith Infra")

	seq := 100
	for _, status := range requestBuckets["open"] {
		seq++
		seedVendorRequest(t, s, ctx, seq, status, requester.ID, mgrID, headID, vendorID, 1000)
	}
	// Every status that is not in the open bucket, so the count is a filter and
	// not just a row count.
	for _, status := range []string{"rejected", "withdrawn", "cancelled", "completed", "completed_partial"} {
		seq++
		seedVendorRequest(t, s, ctx, seq, status, requester.ID, mgrID, headID, vendorID, 1000)
	}

	rows, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors: %v", err)
	}
	if got, want := vendorByName(t, rows, "Harith Infra").OpenRequests, len(requestBuckets["open"]); got != want {
		t.Fatalf("OpenRequests = %d, want %d — one per status in requestBuckets[\"open\"] and nothing else", got, want)
	}
}

// TestVendorPaidThisYearPrefersTheIdAndFallsBackExactly covers the two halves of
// the F-G-009 fix that the rename test cannot reach:
//
//   - the id wins over the text, so a payment carrying somebody else's name in
//     its snapshot is still counted against the vendor it was actually made to;
//   - the payee match survives ONLY for rows with no vendor_id — the pre-v10
//     direct payments, which settle no request and so have nothing to inherit —
//     and is still EXACT, so a near-miss name is never credited to the wrong
//     vendor. That property is older than the fix and must outlive it: on a
//     financial screen a missing row is recoverable, a misattributed one is not.
func TestVendorPaidThisYearPrefersTheIdAndFallsBackExactly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	actor, headID := seedActorAndHead(t, s, ctx)
	today := time.Now().UTC().Format("2006-01-02")

	linked := seedVendorNamed(t, s, "Coastal Power Systems")
	seedVendorNamed(t, s, "Coastal Power")

	// A linked payment whose snapshot names the *other* vendor. The id decides.
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,vendor_id,paid_on,amount,vendor_payee,entered_by)
		VALUES(?,?,?,?,?,?)`, headID, linked, today, 300000, "Coastal Power", actor.ID); err != nil {
		t.Fatal(err)
	}
	// An unlinked payment — vendor_id NULL, the pre-v10 shape — naming the
	// vendor exactly. Only the snapshot can attribute this one.
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID: headID, PaidOn: today, Amount: 120000, VendorPayee: "Coastal Power Systems"}); err != nil {
		t.Fatal(err)
	}
	// An unlinked payment naming a prefix of the vendor. Not the same payee.
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID: headID, PaidOn: today, Amount: 999999, VendorPayee: "Coastal Power Sys"}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListVendors(ctx, VendorListOptions{}, bankBlind())
	if err != nil {
		t.Fatalf("ListVendors: %v", err)
	}
	if got := vendorByName(t, rows, "Coastal Power Systems").PaidThisYear; got != 420000 {
		t.Fatalf("PaidThisYear = %d, want 420000 (300000 by id + 120000 by exact payee; the prefix is not a match)", got)
	}
	if got := vendorByName(t, rows, "Coastal Power").PaidThisYear; got != 0 {
		t.Fatalf("the other vendor's PaidThisYear = %d, want 0 — a linked payment must never be credited by its snapshot text", got)
	}
}
