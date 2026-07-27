package notify

import "testing"

// AllEvents is the order the admin rules screen renders and the seeded
// sort_order it must agree with. The literals are the contract: these exact
// strings are the primary keys in notification_settings, the `ev-{{.Event}}`
// sheet ids on /admin/notifications, and the `event` column store.AddNotification
// derives a notification's kind from. A rename must break a test here, not
// silently stop delivering.
func TestAllEventsIsTheFullCatalogueInSeededOrder(t *testing.T) { // G20, F-F-06
	want := []string{
		// v7 — the twelve of G20.
		"request_submitted", "request_edited", "request_returned", "request_rejected",
		"request_approved", "request_urgent", "request_on_hold", "request_cancellation_requested",
		"payment_settled", "payment_partial_review", "reminder_pending", "reminder_stale_reservation",
		// v9 — the nine the 2026-07-27 audit found missing (F-F-06).
		"request_withdrawn", "request_reraised", "request_unheld",
		"reservation_released", "reservation_reassigned",
		"payment_partial_accepted", "payment_partial_concern",
		"request_cancellation_accepted", "request_cancellation_declined",
		// v11 — the two the repair's own documentation pass found still firing
		// nothing, one of them behind a screen that promised otherwise.
		"approval_reassigned", "request_cancelled",
	}
	if len(AllEvents) != len(want) {
		t.Fatalf("AllEvents = %d events, want %d", len(AllEvents), len(want))
	}
	for i, event := range want {
		if AllEvents[i] != event {
			t.Fatalf("AllEvents[%d] = %q, want %q", i, AllEvents[i], event)
		}
	}
	seen := map[string]bool{}
	for _, event := range AllEvents {
		if seen[event] {
			t.Fatalf("AllEvents lists %q twice; the admin screen would render two sheets with one id", event)
		}
		seen[event] = true
	}
	// The constants and the strings are one thing, checked in both directions.
	for _, pair := range [][2]string{
		{EventRequestWithdrawn, "request_withdrawn"},
		{EventRequestReraised, "request_reraised"},
		{EventRequestUnheld, "request_unheld"},
		{EventReservationReleased, "reservation_released"},
		{EventReservationReassigned, "reservation_reassigned"},
		{EventPaymentPartialAccepted, "payment_partial_accepted"},
		{EventPaymentPartialConcern, "payment_partial_concern"},
		{EventCancellationAccepted, "request_cancellation_accepted"},
		{EventCancellationDeclined, "request_cancellation_declined"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("event constant = %q, want %q", pair[0], pair[1])
		}
	}
}

// Both reminders land on the same request's audit trail, so the row each writes
// has to say which one it was — the stale-reservation screen's banner is
// specifically about the stale one (F-F-02).
func TestReminderLabelDistinguishesTheTwoReminders(t *testing.T) { // F-F-02
	pending := reminderLabel(EventReminderPending)
	stale := reminderLabel(EventReminderStaleReservation)
	if pending == stale {
		t.Fatalf("both reminders label themselves %q", pending)
	}
	for _, label := range []string{pending, stale, reminderLabel("something_else")} {
		if label == "" {
			t.Fatal("a reminder label is blank; the audit summary would read \" sent\"")
		}
	}
}
