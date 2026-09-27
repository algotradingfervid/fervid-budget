// Package notify turns things that happen in the request workflow into
// notifications: an in-app row for every recipient, always, plus an email when
// the admin has enabled that event. It also runs the hourly reminder job and
// holds the SMTP mailer.
//
// This file names the events. Each event is one row in the
// notification_settings table, where an admin chooses who is told (requester,
// approver, Accounts, extra addresses), whether an email goes out, and the
// subject and body text.
package notify

// Event names. The note after each says who is told by default, as seeded;
// an admin can change the recipients on the notification rules screen.
const (
	// Request lifecycle.

	EventRequestSubmitted = "request_submitted" // the approver: a request needs a decision
	EventRequestEdited    = "request_edited"    // the approver: a pending request was changed and re-sent
	EventRequestReturned  = "request_returned"  // the requester: sent back for correction
	EventRequestRejected  = "request_rejected"  // the requester
	EventRequestApproved  = "request_approved"  // the requester and the Accounts group; management is copied on the email
	// EventRequestUrgent goes to the approver while the request is pending and
	// to Accounts (with management copied) once it is approved.
	EventRequestUrgent = "request_urgent"
	EventRequestOnHold = "request_on_hold" // the requester: Accounts put the request on hold
	EventRequestUnheld = "request_unheld"  // the requester: Accounts lifted the hold
	// EventRequestWithdrawn: the requester withdrew a pending request, so the
	// item left the approver's queue without a decision. Told to the approver.
	EventRequestWithdrawn = "request_withdrawn"
	// EventRequestReraised: a rejected request was raised again. Re-raising
	// creates a new request with its own number, so this fires for the new
	// request and tells its approver.
	EventRequestReraised = "request_reraised"
	// EventApproverReassigned: a request was routed to a different approver.
	// By the time it fires the request already names the new approver, so the
	// "approver" recipient is the new one, not their predecessor.
	EventApproverReassigned = "approval_reassigned"
	// EventRequestCancelled: the approver cancelled a request outright (nobody
	// asked for it). Told to the requester, whose money is not coming.
	EventRequestCancelled = "request_cancelled"

	// Cancellation asked for by the requester.

	EventCancellationRequested = "request_cancellation_requested" // the approver and the accountant holding the payment
	// EventCancellationAccepted and EventCancellationDeclined are the two
	// answers to a cancellation request. They are separate events because they
	// say opposite things ("nothing will be paid" / "payment is unfrozen") and
	// each has its own editable text. Both go to the requester and the
	// accountant holding the payment.
	EventCancellationAccepted = "request_cancellation_accepted"
	EventCancellationDeclined = "request_cancellation_declined"

	// Payments.

	EventPaymentSettled       = "payment_settled"        // the requester and the approver
	EventPaymentPartialReview = "payment_partial_review" // the approver: less than approved was paid and needs a decision
	// EventPaymentPartialAccepted: the approver accepted a shortfall and the
	// request closed as completed-partial. The requester learns the balance is
	// not coming; the accountant holding it learns to stop chasing it.
	EventPaymentPartialAccepted = "payment_partial_accepted"
	// EventPaymentPartialConcern: the approver disputed the shortfall instead.
	// It is addressed to the accountant holding the payment and waits on their
	// answer, which is why the store files it under "mention" rather than
	// "activity".
	EventPaymentPartialConcern = "payment_partial_concern"

	// Reservations (an accountant claiming an approved request to pay it).

	EventReservationReleased = "reservation_released" // the requester and the approver: the request is back in the open queue
	// EventReservationReassigned: a reservation was handed to another
	// accountant without going back to the queue. The new holder is told
	// personally, along with the requester and the approver.
	EventReservationReassigned = "reservation_reassigned"

	// Reminders, sent by the hourly reminder job.

	EventReminderPending          = "reminder_pending"           // whoever the request is waiting on (by default the approver)
	EventReminderStaleReservation = "reminder_stale_reservation" // the accountant holding a reservation that has sat too long
)

// AllEvents lists every event in the order the admin rules screen shows them,
// which is the sort_order the database was seeded with. Keep new events at the
// end so the order stays the same.
var AllEvents = []string{
	EventRequestSubmitted, EventRequestEdited, EventRequestReturned, EventRequestRejected,
	EventRequestApproved, EventRequestUrgent, EventRequestOnHold, EventCancellationRequested,
	EventPaymentSettled, EventPaymentPartialReview, EventReminderPending, EventReminderStaleReservation,
	EventRequestWithdrawn, EventRequestReraised, EventRequestUnheld,
	EventReservationReleased, EventReservationReassigned,
	EventPaymentPartialAccepted, EventPaymentPartialConcern,
	EventCancellationAccepted, EventCancellationDeclined,
	EventApproverReassigned, EventRequestCancelled,
}

// reminderLabel names which reminder went out, for the history row
// MarkReminderSent writes on the request. Both reminders can land on the same
// request's history, and two rows both reading "Reminder sent" would not say
// which one it was.
func reminderLabel(event string) string {
	switch event {
	case EventReminderPending:
		return "Pending-approval reminder"
	case EventReminderStaleReservation:
		return "Stale-reservation reminder"
	default:
		return "Reminder"
	}
}
