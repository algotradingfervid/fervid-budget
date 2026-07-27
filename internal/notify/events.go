// Package notify turns things that happen in the request workflow into
// notifications: an in-app row for every recipient, always, plus an email when
// the admin has enabled that event.
package notify

// The twelve events of G20. Keys match the notification_settings rows seeded by
// migration v7; AllEvents is the order the admin rules screen renders.
const (
	EventRequestSubmitted         = "request_submitted"
	EventRequestEdited            = "request_edited"
	EventRequestReturned          = "request_returned"
	EventRequestRejected          = "request_rejected"
	EventRequestApproved          = "request_approved"
	EventRequestUrgent            = "request_urgent"
	EventRequestOnHold            = "request_on_hold"
	EventCancellationRequested    = "request_cancellation_requested"
	EventPaymentSettled           = "payment_settled"
	EventPaymentPartialReview     = "payment_partial_review"
	EventReminderPending          = "reminder_pending"
	EventReminderStaleReservation = "reminder_stale_reservation"
)

var AllEvents = []string{
	EventRequestSubmitted, EventRequestEdited, EventRequestReturned, EventRequestRejected,
	EventRequestApproved, EventRequestUrgent, EventRequestOnHold, EventCancellationRequested,
	EventPaymentSettled, EventPaymentPartialReview, EventReminderPending, EventReminderStaleReservation,
}

// The plan also listed a kindFor(event) classifier here. It is deliberately
// absent: store.AddNotification already derives kind from the event at write
// time, and a second copy in this package could only ever drift from the one
// the .segmented filter actually queries.
