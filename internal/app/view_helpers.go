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

// pillClasses are the `.pill` CSS classes whose name differs from the status.
// Every other status is its own class name.
var pillClasses = map[string]string{
	"pending":                "awaiting",
	"cancellation_requested": "cancelreq",
	"withdrawn":              "cancelled",
	"partial_review":         "partial",
	"completed_partial":      "completed-partial",
}

// pillClass is the `.pill` CSS class for a request status.
func pillClass(status string) string {
	if class, ok := pillClasses[status]; ok {
		return class
	}
	return status
}

// requestStatusTexts are the words a person reads for each request status.
var requestStatusTexts = map[string]string{
	"pending":                "Awaiting approval",
	"returned":               "Returned for correction",
	"approved":               "Approved — awaiting payment",
	"rejected":               "Rejected — final",
	"withdrawn":              "Withdrawn",
	"cancellation_requested": "Cancellation requested",
	"cancelled":              "Cancelled",
	"processing":             "With Accounts",
	"partial_review":         "Partial — manager review",
	"completed":              "Completed",
	"completed_partial":      "Completed — partial accepted",
}

// requestStatusText is the status as a person reads it; an unknown status is
// shown as stored. (statusText is a different helper, for the budget grid.)
func requestStatusText(status string) string {
	if text, ok := requestStatusTexts[status]; ok {
		return text
	}
	return status
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
	if label, ok := threadFieldLabels[column]; ok {
		return label
	}
	return strings.ReplaceAll(column, "_", " ")
}

var threadFieldLabels = map[string]string{
	"amount": "Amount", "approved_amount": "Approved amount", "needed_by": "Needed by",
	"invoice_no": "Invoice number", "invoice_date": "Invoice date", "expense_date": "Expense date",
	"manager_id": "Approver", "short_title": "Short title", "purpose": "Purpose",
	"urgent": "Urgency",
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

// auditStyle is how one audit action is drawn on a history trail: the colour
// class of its dot, the symbol inside the dot, and the verb phrase printed after
// the actor's name ("Priya Nair reserved it for processing"). These trails are
// built from audit rows; the request conversation uses threadDot/threadGlyph
// instead, which differ for a few actions.
type auditStyle struct{ Tone, Glyph, Phrase string }

// auditStyles has one line per audit action. An empty field falls back: no
// tone, the "·" glyph, or the generic label from actionText. The attach glyph
// is a text symbol rather than an emoji, so the CSS can tint it like the rest.
var auditStyles = map[string]auditStyle{
	"submit":            {"brand", "＋", "submitted the request"},
	"reraise":           {"brand", "＋", "raised the request again"},
	"process":           {"brand", "◷", "reserved it for processing"},
	"reassign":          {"brand", "◷", "reassigned the reservation"},
	"approval_reassign": {"brand", "✎", "reassigned the approval"},
	"unhold":            {"brand", "◷", "took the request off hold"}, // back in the takeable queue
	"approve":           {"ok", "✓", "approved the request"},
	"settle":            {"ok", "✓", "settled the request"},
	"accept_partial":    {"ok", "✓", "accepted the partial payment"},
	"record_payment":    {"ok", "₹", "recorded a payment"},
	"hold":              {"warn", "⏸", "put the request on hold"},
	"mark_partial":      {"warn", "₹", "marked it a partial payment"},
	"release":           {"warn", "↩", "released the reservation"},
	"concern":           {"warn", "₹", "raised a concern"},
	"return":            {"warn", "↩", "returned the request for correction"},
	"reject":            {"bad", "✕", "rejected the request"},
	"cancel":            {"bad", "✕", "cancelled the request"},
	"withdraw":          {"bad", "✕", "withdrew the request"},
	"void":              {"bad", "✕", "voided the payment"},
	"create":            {"", "＋", ""},
	"update":            {"", "✎", "edited the request"},
	"amend_payment":     {"", "✎", "corrected the payment"},
	"attach":            {"", "⇪", "attached a document"},
	"attach_payment":    {"", "⇪", "attached proof of payment"},
	"comment":           {"", "", "commented"},
	"cancel_request":    {"", "", "asked for the request to be cancelled"},
}

// auditTone is the colour class of an audit action's dot ("" for none).
func auditTone(action string) string { return auditStyles[action].Tone }

// auditGlyph is the symbol in an audit action's dot.
func auditGlyph(action string) string {
	if glyph := auditStyles[action].Glyph; glyph != "" {
		return glyph
	}
	return "·"
}

// auditPhrase is the verb phrase that follows the actor's name on a trail.
func auditPhrase(action string) string {
	if phrase := auditStyles[action].Phrase; phrase != "" {
		return phrase
	}
	return actionText(action)
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
