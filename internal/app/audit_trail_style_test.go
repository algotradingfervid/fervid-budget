package app

import "testing"

// TestAuditTrailStyles pins the dot colour, dot symbol and verb phrase that
// history trails show for every audit action the app knows, plus the empty
// string and an unknown action, so the audit-action table can be reshaped
// without changing what a trail looks like.
func TestAuditTrailStyles(t *testing.T) {
	want := map[string][3]string{ // action -> {auditTone, auditGlyph, auditPhrase}
		"":                  {"", "·", ""},
		"accept_partial":    {"ok", "✓", "accepted the partial payment"},
		"amend_payment":     {"", "✎", "corrected the payment"},
		"approval_reassign": {"brand", "✎", "reassigned the approval"},
		"approve":           {"ok", "✓", "approved the request"},
		"attach":            {"", "⇪", "attached a document"},
		"attach_payment":    {"", "⇪", "attached proof of payment"},
		"cancel":            {"bad", "✕", "cancelled the request"},
		"cancel_request":    {"", "·", "asked for the request to be cancelled"},
		"comment":           {"", "·", "commented"},
		"concern":           {"warn", "₹", "raised a concern"},
		"create":            {"", "＋", "Created"},
		"delete":            {"", "·", "Deleted"},
		"edit":              {"", "·", "Updated"},
		"export":            {"", "·", "Exported"},
		"hold":              {"warn", "⏸", "put the request on hold"},
		"lock":              {"", "·", "Locked"},
		"login":             {"", "·", "Logged in"},
		"login_failed":      {"", "·", "Login failed"},
		"logout":            {"", "·", "Logged out"},
		"mark_partial":      {"warn", "₹", "marked it a partial payment"},
		"no_such_action":    {"", "·", "no_such_action"},
		"process":           {"brand", "◷", "reserved it for processing"},
		"reassign":          {"brand", "◷", "reassigned the reservation"},
		"record_payment":    {"ok", "₹", "recorded a payment"},
		"recovery":          {"", "·", "Recovery recorded"},
		"reject":            {"bad", "✕", "rejected the request"},
		"release":           {"warn", "↩", "released the reservation"},
		"remind":            {"", "·", "Reminder sent"},
		"reraise":           {"brand", "＋", "raised the request again"},
		"reserve":           {"", "·", "Reserved"},
		"return":            {"warn", "↩", "returned the request for correction"},
		"settings":          {"", "·", "Settings saved"},
		"settle":            {"ok", "✓", "settled the request"},
		"submit":            {"brand", "＋", "submitted the request"},
		"unhold":            {"brand", "◷", "took the request off hold"},
		"unlock":            {"", "·", "Unlocked"},
		"update":            {"", "✎", "edited the request"},
		"view":              {"", "·", "Viewed"},
		"void":              {"bad", "✕", "voided the payment"},
		"withdraw":          {"bad", "✕", "withdrew the request"},
	}
	for action, w := range want {
		if got := auditTone(action); got != w[0] {
			t.Errorf("auditTone(%q) = %q, want %q", action, got, w[0])
		}
		if got := auditGlyph(action); got != w[1] {
			t.Errorf("auditGlyph(%q) = %q, want %q", action, got, w[1])
		}
		if got := auditPhrase(action); got != w[2] {
			t.Errorf("auditPhrase(%q) = %q, want %q", action, got, w[2])
		}
	}
}

// TestTrailOnlyActionLabels pins the audit-log label and pill class of the
// actions only history trails know about: they have no label of their own,
// so the audit log shows the raw action in a neutral pill, and the Action
// filter does not offer them.
func TestTrailOnlyActionLabels(t *testing.T) {
	for _, action := range []string{"record_payment", "amend_payment", "attach_payment"} {
		if got := actionText(action); got != action {
			t.Errorf("actionText(%q) = %q, want %q", action, got, action)
		}
		if got := actionClass(action); got != "neutral" {
			t.Errorf("actionClass(%q) = %q, want neutral", action, got)
		}
		for _, o := range auditActions() {
			if o.Value == action {
				t.Errorf("auditActions offers %q, want it left out", action)
			}
		}
	}
}
