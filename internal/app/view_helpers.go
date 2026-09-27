package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// Template helpers for the request, payment and recoverable screens. Each one
// turns a stored value (a status, an audit action, a time, an amount) into the
// words or CSS class a person sees. They are registered in the template
// FuncMap, so every screen spells a status or an action the same way.

// Waiting is the "waiting on" line: one plain sentence naming who owes the
// next action on a request. Class is the Phase 0 modifier — "you" in brand
// colour when it is the reader, "closed" when nobody owes anything any more.
// It is computed once, here, so the list, the approvals queue and the detail
// head cannot drift into three different answers.
type Waiting struct {
	Text  string
	Class string
}

func waitingOn(req store.Request, viewerID int64) Waiting {
	you := Waiting{Text: "Waiting on you", Class: "you"}
	// A hold is not a status — the request stays 'approved' so the queue's own
	// availability test takes it out of the takeable set (L7) — but it is the
	// only thing anybody is waiting on while it lasts. It therefore answers
	// before the status does, or a held request would read "Waiting on
	// Accounts" while Accounts is the one waiting.
	//
	// activeHold, not req.OnHold: answering before the status is only right
	// while 'approved' is the status. On anything else there is a real decision
	// pending, and hiding it behind the hold would take the approvals queue's
	// answer away from the one person who owes it.
	if activeHold(req) {
		if req.RequesterID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.RequesterName + " to clarify"}
	}
	switch req.Status {
	case "pending", "cancellation_requested":
		if req.ManagerID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "returned":
		if req.RequesterID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.RequesterName}
	case "approved":
		return Waiting{Text: "Waiting on Accounts"}
	case "rejected":
		return Waiting{Text: "Closed. Raise a new request if needed", Class: "closed"}
	case "withdrawn":
		return Waiting{Text: "Withdrawn by the requester", Class: "closed"}
	case "cancelled":
		return Waiting{Text: "Cancelled. Nothing can be paid against it", Class: "closed"}
	case "processing":
		if req.ProcessingBy != nil && *req.ProcessingBy == viewerID {
			return you
		}
		// The assigned accountant by name (uiux design §4, "Waiting on Priya
		// Nair (Accounts)"), not the department: the reader wants to know who to
		// ask. The generic line stays for a row read without the join.
		if req.ProcessingByName != "" {
			return Waiting{Text: "Waiting on " + req.ProcessingByName + " (Accounts)"}
		}
		return Waiting{Text: "Waiting on Accounts"}
	case "partial_review":
		// An open concern is a question the manager put to Accounts, so both
		// owe something: Accounts an answer, the manager the decision that
		// follows it (owner decision, settlement-8). The holder is who answers.
		if req.ConcernOpen {
			switch {
			case req.ProcessingBy != nil && *req.ProcessingBy == viewerID:
				return you
			case req.ManagerID == viewerID:
				return Waiting{Text: "Waiting on you and Accounts", Class: "you"}
			default:
				return Waiting{Text: "Waiting on " + req.ManagerName + " and Accounts"}
			}
		}
		if req.ManagerID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "completed", "completed_partial":
		return Waiting{Text: "Nothing pending", Class: "done"}
	default:
		return Waiting{}
	}
}

// pillClass maps a stored status onto the Phase 0 `.pill` modifier. Phase 0
// owns the CSS; this owns nothing but the mapping.
func pillClass(status string) string {
	switch status {
	case "pending":
		return "awaiting"
	case "cancellation_requested":
		return "cancelreq"
	case "withdrawn":
		return "cancelled"
	// The Phase-3 statuses whose enum name is not the design system's class
	// name. Everything else already matches, and a status with no rule of its
	// own would render an invisible pill.
	case "partial_review":
		return "partial"
	case "completed_partial":
		return "completed-partial"
	default:
		return status
	}
}

// requestStatusText is the sentence a person reads, not the enum value. It is
// deliberately not called statusText: that name already belongs to the variance
// grid's budget status, and one word meaning two things is how they drift.
func requestStatusText(status string) string {
	switch status {
	case "pending":
		return "Awaiting approval"
	case "returned":
		return "Returned for correction"
	case "approved":
		return "Approved — awaiting payment"
	case "rejected":
		return "Rejected — final"
	case "withdrawn":
		return "Withdrawn"
	case "cancellation_requested":
		return "Cancellation requested"
	case "cancelled":
		return "Cancelled"
	case "processing":
		return "With Accounts"
	case "partial_review":
		return "Partial — manager review"
	case "completed":
		return "Completed"
	case "completed_partial":
		return "Completed — partial accepted"
	default:
		return status
	}
}

// Pill is the status pill as a reader sees it: the `.pill` modifier and the
// sentence inside it.
type Pill struct{ Class, Text string }

// statusPill reads the row, not only the status, because two of the design's
// states are not statuses (uiux design §4): "Processing — taken by *name*"
// names the holder, and "Partial — under discussion" is a partial review with
// an open concern. Every screen that shows a request's pill uses this, so the
// detail head, the cards, the queue and the dashboard cannot disagree about
// who has it. pillClass/requestStatusText stay for the places that only have
// a status string.
func statusPill(req store.Request, viewerID int64) Pill {
	switch {
	case req.Status == "processing" && req.ProcessingByName != "":
		holder := req.ProcessingByName
		if req.ProcessingBy != nil && *req.ProcessingBy == viewerID {
			holder = "you"
		}
		return Pill{Class: "processing", Text: requestStatusText(req.Status) + " — taken by " + holder}
	case req.Status == "partial_review" && req.ConcernOpen:
		return Pill{Class: "discussion", Text: "Partial — under discussion"}
	default:
		return Pill{Class: pillClass(req.Status), Text: requestStatusText(req.Status)}
	}
}

// rowStatusPill is statusPill for a recoverables register row, which is not a
// store.Request but carries the same three facts the pill turns on — status,
// holder and open concern — so the register cannot describe a request that is
// taken or under discussion differently from the detail page behind it.
func rowStatusPill(row store.RecoverableRow, viewerID int64) Pill {
	return statusPill(store.Request{
		Status:           row.Status,
		ProcessingBy:     row.ProcessingBy,
		ProcessingByName: row.ProcessingByName,
		ConcernOpen:      row.ConcernOpen,
	}, viewerID)
}

func typeLabel(t string) string {
	if label, ok := requestTypeLabels[t]; ok {
		return label
	}
	return t
}

// recoverableCategoryLabels names the Phase-2 recoverable categories. Phase 4
// replaces the map with rows from `recoverable_categories`.
var recoverableCategoryLabels = map[string]string{
	"emd":              "EMD — earnest money deposit",
	"pbg":              "PBG — performance bank guarantee",
	"icd":              "ICD — inter-corporate deposit",
	"employee_advance": "Employee advance",
	"security_deposit": "Security deposit",
	"other":            "Other",
}

func recoverableLabel(code string) string {
	if label, ok := recoverableCategoryLabels[code]; ok {
		return label
	}
	return code
}

// formatLongDate turns a stored YYYY-MM-DD into the date a person would say.
func formatLongDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2 January 2006")
}

// amountValue renders paise for a `.money-field` input, which draws its own ₹
// in the `.cur` prefix — so the value must not carry a second one.
func amountValue(paise int64) string {
	if paise <= 0 {
		return ""
	}
	return strings.TrimPrefix(money.FormatPaise(paise), "₹")
}

// threadDot and threadGlyph decorate one line of the merged thread. Kind wins
// over Action: a comment is shown as the person who wrote it, whatever the
// audit called the event behind it.
//
// The hold pair is here because the hold lives on this stream, not on a trail
// of its own: request-on-hold.html renders it as a warn ⏸, and "unhold" is a
// return to the takeable queue, which is what auditTone tones "brand". The two
// helpers describe the same two events and have to agree about them.
func threadDot(e store.ThreadEntry) string {
	switch e.Action {
	case "submit", "reraise", "unhold":
		return "brand"
	case "approve":
		return "ok"
	case "return", "cancel_request", "cancel_decline", "hold":
		return "warn"
	case "reject", "cancel", "withdraw":
		return "bad"
	default:
		return ""
	}
}

func threadGlyph(e store.ThreadEntry) string {
	switch e.Kind {
	case "comment":
		return e.Initials
	case "attachment":
		return "⇪"
	}
	switch e.Action {
	case "submit", "reraise":
		return "＋"
	case "approve":
		return "✓"
	case "return":
		return "↩"
	case "reject", "withdraw":
		return "✕"
	case "update":
		return "✎"
	case "cancel", "cancel_request", "cancel_decline", "hold":
		return "⏸"
	case "unhold":
		return "◷"
	default:
		return "·"
	}
}

// threadValue renders one side of a `.tl-change` the way a person reads it. The
// audit trail stores raw JSON, so an amount arrives as 2.35e+07 and a flag as
// true — neither is a sentence anybody wants to find in their own history.
func threadValue(field, raw string) string {
	switch raw {
	case "", "<nil>", "null":
		return "—"
	}
	switch field {
	case "amount", "approved_amount":
		if paise, err := strconv.ParseFloat(raw, 64); err == nil {
			return money.FormatPaise(int64(paise))
		}
	case "urgent":
		if raw == "true" || raw == "1" {
			return "Urgent"
		}
		return "Normal"
	}
	return raw
}

// threadField names a changed column for a reader. The audit records column
// names, which are the right key and the wrong words.
func threadField(column string) string {
	if label, ok := map[string]string{
		"amount": "Amount", "approved_amount": "Approved amount", "needed_by": "Needed by",
		"invoice_no": "Invoice number", "invoice_date": "Invoice date", "expense_date": "Expense date",
		"manager_id": "Approver", "short_title": "Short title", "purpose": "Purpose",
		"urgent": "Urgency",
	}[column]; ok {
		return label
	}
	return strings.ReplaceAll(column, "_", " ")
}

// fileKind is the three-or-four letter tag the `.f-ico` square shows.
func fileKind(name string) string {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		return "FILE"
	}
	if len(ext) > 4 {
		ext = ext[:4]
	}
	return ext
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// activeHold is the hold as a reader experiences it, rather than the column.
// Every writer that moves a request off 'approved' clears the hold, so the two
// are the same thing on a well-formed row — but the pill, the waiting line and
// the release control all lie the moment they disagree, and a row written
// before that rule existed is exactly when they would. Asking one question in
// one place is what keeps the answer from drifting between them.
func activeHold(req store.Request) bool {
	return req.OnHold && req.Status == "approved"
}

// approvedOf is the ceiling a payment is measured against: what the approver
// signed off, falling back to what was asked when nothing was recorded.
func approvedOf(req store.Request) int64 {
	if req.ApprovedAmount != nil {
		return *req.ApprovedAmount
	}
	return req.Amount
}

func subPaise(a, b int64) int64 { return a - b }

func hhmm(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format("15:04")
}

func since(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Since(*t)
	if d < time.Hour {
		if m := int(d.Minutes()); m > 0 {
			return fmt.Sprintf("%d m", m)
		}
		return "just now"
	}
	return fmt.Sprintf("%d h", int(d.Hours()))
}

// reservedLabel is the clock time while the reservation is same-day and the
// elapsed time once it is older. "· 14:02" answers "when did I start?"; "· 26 h"
// answers "how long has this been sitting?".
func reservedLabel(t *time.Time) string {
	if t == nil {
		return ""
	}
	if time.Since(*t) < store.StaleReservation {
		return hhmm(t)
	}
	return since(t)
}

// stale is the predicate reservedLabel switches on, exposed on its own so a row
// that has crossed the one-day mark can be linked to the nudge screen (Q6)
// rather than merely labelled with the elapsed hours. The count in
// LinkableSet.Counts is an aggregate over the whole scope and cannot say which
// row it means; this can.
func stale(t *time.Time) bool {
	return t != nil && time.Since(*t) >= store.StaleReservation
}

// trailAction disambiguates an audit row before it is decorated or spelled. The
// full trail merges two entity types, and "create" or "update" on a payment is a
// different sentence from the same word on a request — so the entity is folded
// into the action once, here, rather than in every switch downstream. Every name
// it invents is one no store writer uses, so nothing else changes meaning.
func trailAction(e store.AuditEntry) string {
	if e.EntityType != "payment" {
		return e.Action
	}
	switch e.Action {
	case "create":
		return "record_payment"
	case "update":
		return "amend_payment"
	case "attach":
		return "attach_payment"
	default:
		return e.Action
	}
}

// auditTone and auditGlyph decorate one `.tl-dot` of a trail built from audit
// rows. They are deliberately not threadDot/threadGlyph: those two take a
// store.ThreadEntry and belong to the request conversation, and one name meaning
// two things is how they drift.
func auditTone(action string) string {
	switch action {
	// "unhold" sits with the reservation movements: lifting a hold puts the
	// request back in the takeable queue, which is the same kind of event.
	case "submit", "reraise", "process", "reassign", "approval_reassign", "unhold":
		return "brand"
	case "approve", "settle", "accept_partial", "record_payment":
		return "ok"
	case "hold", "mark_partial", "release", "concern", "return":
		return "warn"
	case "reject", "cancel", "withdraw", "void":
		return "bad"
	default:
		return ""
	}
}

func auditGlyph(action string) string {
	switch action {
	case "submit", "reraise", "create":
		return "＋"
	case "approve", "settle", "accept_partial":
		return "✓"
	case "process", "reassign", "unhold":
		return "◷"
	case "hold":
		return "⏸"
	case "mark_partial", "concern", "record_payment":
		return "₹"
	case "release", "return":
		return "↩"
	case "reject", "cancel", "withdraw", "void":
		return "✕"
	case "update", "amend_payment", "approval_reassign":
		return "✎"
	case "attach", "attach_payment":
		// The thread's own upload glyph. Every dot in a `.thread` is a
		// monochrome text symbol the CSS tints with `color:`; an emoji ignores
		// that and lands as a colour sticker in a column of grey marks.
		return "⇪"
	default:
		return "·"
	}
}

// auditPhrase is the trail's headline, written to follow the actor's name:
// "Priya Nair reserved it for processing". The audit summary underneath carries
// the figures; this line carries the verb, so a reader skimming the left column
// of the thread gets the story without reading a single amount.
func auditPhrase(action string) string {
	switch action {
	case "submit":
		return "submitted the request"
	case "update":
		return "edited the request"
	case "attach":
		return "attached a document"
	case "comment":
		return "commented"
	case "approve":
		return "approved the request"
	case "return":
		return "returned the request for correction"
	case "reject":
		return "rejected the request"
	case "withdraw":
		return "withdrew the request"
	case "reraise":
		return "raised the request again"
	case "cancel_request":
		return "asked for the request to be cancelled"
	case "cancel":
		return "cancelled the request"
	case "process":
		return "reserved it for processing"
	case "release":
		return "released the reservation"
	case "reassign":
		return "reassigned the reservation"
	case "approval_reassign":
		return "reassigned the approval"
	case "hold":
		return "put the request on hold"
	case "unhold":
		return "took the request off hold"
	case "concern":
		return "raised a concern"
	case "settle":
		return "settled the request"
	case "mark_partial":
		return "marked it a partial payment"
	case "accept_partial":
		return "accepted the partial payment"
	case "record_payment":
		return "recorded a payment"
	case "amend_payment":
		return "corrected the payment"
	case "attach_payment":
		return "attached proof of payment"
	case "void":
		return "voided the payment"
	default:
		return actionText(action)
	}
}

// trailBody is the audit summary as the trail prints it, under a head that has
// already named the actor. Every request-side writer composes its summary as
// "<actor> did this", so rendering both put the name on the screen twice; the
// mockup's bodies carry the figures and nothing else. The name is removed only
// when the summary genuinely opens with it — payment-side summaries ("Recorded
// payment ₹98,000.00") never do, and come through untouched.
func trailBody(e store.AuditEntry) string {
	body := e.Summary
	if e.ActorName != "" {
		rest := strings.TrimPrefix(body, e.ActorName+" ")
		if rest != body && rest != "" {
			letters := []rune(rest)
			letters[0] = unicode.ToUpper(letters[0])
			body = string(letters)
		}
	}
	if note := store.RequestAuditNote(e); note != "" {
		body += " — Note: " + note
	}
	return body
}

// initials is the two-letter avatar a comment's `.tl-dot` shows.
func initials(name string) string {
	out := make([]rune, 0, 2)
	for _, word := range strings.Fields(name) {
		for _, letter := range word {
			out = append(out, unicode.ToUpper(letter))
			break
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "··"
	}
	return string(out)
}
