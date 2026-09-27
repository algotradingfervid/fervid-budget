package app

import (
	"reflect"
	"testing"
)

// These tests pin the words and CSS classes the display helpers produce for
// every value they know, plus an unknown value and the empty string, so the
// helpers can be rewritten as lookup tables without changing a single label.

func TestAuditActionLabelsAndClasses(t *testing.T) {
	want := map[string][2]string{ // action -> {actionText, actionClass}
		"create": {"Created", "good"}, "update": {"Updated", "info"}, "edit": {"Updated", "info"},
		"delete": {"Deleted", "neutral"}, "attach": {"Attached", "info"}, "comment": {"Commented", "info"},
		"submit": {"Submitted", "good"}, "approve": {"Approved", "good"}, "return": {"Returned", "warn"},
		"reject": {"Rejected", "bad"}, "withdraw": {"Withdrawn", "warn"}, "reraise": {"Raised again", "info"},
		"cancel_request": {"Cancellation asked", "neutral"}, "cancel": {"Cancelled", "bad"},
		"approval_reassign": {"Approval reassigned", "info"}, "process": {"Reserved", "info"},
		"reserve": {"Reserved", "info"}, "release": {"Released", "info"},
		"reassign": {"Reservation reassigned", "info"}, "hold": {"Put on hold", "warn"},
		"unhold": {"Taken off hold", "good"}, "settle": {"Settled", "good"},
		"mark_partial": {"Marked partial", "warn"}, "accept_partial": {"Partial accepted", "good"},
		"concern": {"Concern raised", "bad"}, "remind": {"Reminder sent", "info"},
		"recovery": {"Recovery recorded", "neutral"}, "void": {"Voided", "bad"},
		"lock": {"Locked", "warn"}, "unlock": {"Unlocked", "warn"}, "settings": {"Settings saved", "info"},
		"export": {"Exported", "neutral"}, "login": {"Logged in", "neutral"},
		"logout": {"Logged out", "neutral"}, "login_failed": {"Login failed", "bad"},
		"view": {"Viewed", "neutral"}, "": {"", "neutral"}, "no_such_action": {"no_such_action", "neutral"},
	}
	for action, w := range want {
		if got := actionText(action); got != w[0] {
			t.Errorf("actionText(%q) = %q, want %q", action, got, w[0])
		}
		if got := actionClass(action); got != w[1] {
			t.Errorf("actionClass(%q) = %q, want %q", action, got, w[1])
		}
	}
}

func TestAuditFilterOptionsOrder(t *testing.T) {
	var actions []string
	for _, o := range auditActions() {
		actions = append(actions, o.Value)
		if o.Label != actionText(o.Value) {
			t.Errorf("auditActions label for %q = %q, want %q", o.Value, o.Label, actionText(o.Value))
		}
	}
	wantActions := []string{
		"create", "update", "delete", "attach", "comment",
		"submit", "approve", "return", "reject", "withdraw", "reraise",
		"cancel_request", "cancel", "approval_reassign",
		"process", "release", "reassign", "hold", "unhold",
		"settle", "mark_partial", "accept_partial", "concern", "remind",
		"recovery", "void", "lock", "unlock", "settings", "export",
		"login", "logout", "login_failed",
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Errorf("auditActions order = %v, want %v", actions, wantActions)
	}
	var entities []auditOption
	entities = append(entities, auditEntities()...)
	if len(entities) != 16 || entities[0] != (auditOption{"payment_request", "Payment Request"}) ||
		entities[15] != (auditOption{"backup", "Backup"}) {
		t.Errorf("auditEntities = %v", entities)
	}
}

func TestEntityText(t *testing.T) {
	for in, want := range map[string]string{
		"payment_request": "Payment Request", "app_setting": "App Setting", "backup": "Backup",
		"": "", "a-b.c": "A-B.C", "x9y z": "X9y Z", "already Title": "Already Title",
		"ünïcode word": "Ünïcode Word", "a·b": "A·b",
	} {
		if got := entityText(in); got != want {
			t.Errorf("entityText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValueLabels(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) string
		want map[string]string
	}{
		{"statusText", statusText, map[string]string{
			"no-activity": "No activity", "over": "Over budget", "under": "Under budget",
			"on-track": "On track", "not-paid": "Not paid", "unbudgeted": "Unbudgeted spend",
			"": "", "odd": "odd"}},
		{"planStatusText", planStatusText, map[string]string{"locked": "Locked", "open": "Open", "": "", "odd": "odd"}},
		{"paymentModeText", paymentModeText, map[string]string{
			"neft": "NEFT", "rtgs": "RTGS", "dd": "Demand draft", "bank_transfer": "Bank transfer",
			"cash": "Cash", "cheque": "Cheque", "card": "Card", "upi": "UPI", "other": "Other",
			"NEFT": "NEFT", "Bank transfer": "Bank transfer", "": "", "Barter": "Barter"}},
		{"vendorTypeText", vendorTypeText, map[string]string{
			"company": "Company", "proprietor": "Proprietor", "individual": "Individual", "": "", "odd": "odd"}},
		{"vendorStatusText", vendorStatusText, map[string]string{"active": "Active", "inactive": "Inactive", "": "", "odd": "odd"}},
		{"barClass", barClass, map[string]string{
			"over": "is-over", "under": "is-under", "on-track": "is-ontrack", "unbudgeted": "is-unbudgeted",
			"": "", "not-paid": ""}},
	}
	for _, c := range cases {
		for in, want := range c.want {
			if got := c.fn(in); got != want {
				t.Errorf("%s(%q) = %q, want %q", c.name, in, got, want)
			}
		}
	}
}
