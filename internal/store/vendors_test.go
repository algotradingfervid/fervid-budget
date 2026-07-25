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
