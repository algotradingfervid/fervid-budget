// Package notify turns things that happen in the request workflow into
// notifications: an in-app row for every recipient, always, plus an email when
// the admin has enabled that event.
package notify

// The twelve events of G20, seeded by migration v7.
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

// The nine events the 2026-07-27 audit found missing (F-F-06), seeded by
// migration v9. Six real workflow actions notified nobody at all — withdraw,
// re-raise, release, unhold, accept-partial and either cancellation decision —
// and three more sat in the same gap: handing a reservation to a colleague,
// raising a concern about a shortfall, and the two halves of a cancellation
// decision needing to say opposite things.
//
// Each of these names a transition that changes what a *specific other person*
// has to do next, which is the test for whether it deserves an event at all:
// the approver whose queue item vanished on a withdrawal, the requester whose
// invoice is unclaimed again after a release, the accountant told to stop
// chasing a balance somebody has written off.
const (
	// EventRequestWithdrawn — the requester withdrew a pending request. Told to
	// the approver, whose queue item disappeared without a decision.
	EventRequestWithdrawn = "request_withdrawn"
	// EventRequestReraised — a rejected request was raised again. D1 makes this
	// a *new* request with its own number, so it is fired for the new id and the
	// approver would otherwise never learn the second attempt exists.
	EventRequestReraised = "request_reraised"
	// EventRequestUnheld — Accounts lifted a hold. request_on_hold already
	// tells the requester the hold was placed; this is the other half.
	EventRequestUnheld = "request_unheld"
	// EventReservationReleased — an accountant gave a reservation back to the
	// open queue. The release screen states in as many words that "the requester
	// and the approver are both notified", so both are.
	EventReservationReleased = "reservation_released"
	// EventReservationReassigned — a reservation changed hands without going
	// back to the queue (G11). The new assignee is addressed personally, since
	// they are the one now expected to pay it.
	EventReservationReassigned = "reservation_reassigned"
	// EventPaymentPartialAccepted — the approver accepted a shortfall and the
	// request closed as completed_partial. The requester learns the balance is
	// never coming; the assigned accountant learns to stop chasing it.
	EventPaymentPartialAccepted = "payment_partial_accepted"
	// EventPaymentPartialConcern — the approver disputed the shortfall instead.
	// This one is addressed to the accountant and waits on their answer, which
	// is why store.notificationKind files it under "mention" and not "activity".
	EventPaymentPartialConcern = "payment_partial_concern"
	// EventCancellationAccepted / EventCancellationDeclined — the two halves of
	// deciding a cancellation ask. They are two events, not one, because the
	// sentences are opposites ("nothing will be paid" / "payment is unfrozen")
	// and each is an admin-editable template in its own right.
	EventCancellationAccepted = "request_cancellation_accepted"
	EventCancellationDeclined = "request_cancellation_declined"
)

// The two the audit's own repair left behind, found while reconciling the
// documentation against the code that had just been written. Both are the same
// mistake the nine above were: a transition that changes what a specific other
// person must do next, firing nothing.
const (
	// EventApproverReassigned — a request was routed to a different approver.
	// The reassignment sheet promises, in as many words, that "The new approver
	// is told, and the reminder clock starts again"; the route Wave 3 built to
	// make that reassignment possible at all fired nothing, so the sentence was
	// false from the moment it shipped. Told to the new approver, who is the
	// only person with something to do: ReassignRequest has already moved
	// manager_id, so IncludeManager addresses them and not their predecessor.
	EventApproverReassigned = "approval_reassigned"
	// EventRequestCancelled — the approver cancelled a request outright, which
	// is a different act from deciding a cancellation the requester asked for
	// (that pair is above). Nobody asked, so the requester learns here that the
	// money they were waiting on is not coming.
	EventRequestCancelled = "request_cancelled"
)

// AllEvents is the order the admin rules screen renders, which is the seeded
// sort_order: v7's twelve, then v9's nine, then v11's two.
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

// The plan also listed a kindFor(event) classifier here. It is deliberately
// absent: store.AddNotification already derives kind from the event at write
// time, and a second copy in this package could only ever drift from the one
// the .segmented filter actually queries.

// reminderLabel names which reminder went out, for the audit row
// MarkReminderSent writes (F-F-02). Both reminders land on the same request's
// trail, and two rows both reading "Reminder sent" tell a reader nothing —
// least of all on the stale-reservation screen, whose banner is specifically
// about the stale one.
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
