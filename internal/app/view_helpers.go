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

// Waiting is the "waiting on" line of a request: a short sentence naming who
// must act next, and a CSS class ("you" when it is the reader, "closed" or
// "done" when nobody has to act).
type Waiting struct {
	Text  string
	Class string
}

// waitingOn works out the "waiting on" line for req as seen by viewerID. The
// request list, the approvals queue and the request page all use it.
func waitingOn(req store.Request, viewerID int64) Waiting {
	you := Waiting{Text: "Waiting on you", Class: "you"}
	// An approved request on hold waits on the requester to answer Accounts'
	// question, whatever else is true.
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
		// Name the accountant who reserved it, when the name was loaded.
		if req.ProcessingByName != "" {
			return Waiting{Text: "Waiting on " + req.ProcessingByName + " (Accounts)"}
		}
		return Waiting{Text: "Waiting on Accounts"}
	case "partial_review":
		// With an open concern both must act: the accountant holding it must
		// answer, then the approver decides.
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

// Pill is a status pill: its CSS class and its text.
type Pill struct{ Class, Text string }

// statusPill is the pill for a request. Two cases need more than the status:
// a reserved request names who has it ("With Accounts — taken by you"), and a
// partial review with an open concern reads "Partial — under discussion".
// Every screen showing a request's pill uses this.
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

// rowStatusPill is statusPill for a row of the recoverables list.
func rowStatusPill(row store.RecoverableRow, viewerID int64) Pill {
	return statusPill(store.Request{
		Status:           row.Status,
		ProcessingBy:     row.ProcessingBy,
		ProcessingByName: row.ProcessingByName,
		ConcernOpen:      row.ConcernOpen,
	}, viewerID)
}

// typeLabel is the label of a request type, or the type itself if unknown.
func typeLabel(t string) string {
	if label, ok := requestTypeLabels[t]; ok {
		return label
	}
	return t
}

// recoverableCategoryLabels are the names of the six seeded recoverable
// categories. They are used where the category rows are not loaded (the
// "recoverable" template helper and the legacy-deposit warning). Categories an
// admin adds or renames later show their code there instead.
var recoverableCategoryLabels = map[string]string{
	"emd":              "EMD — earnest money deposit",
	"pbg":              "PBG — performance bank guarantee",
	"icd":              "ICD — inter-corporate deposit",
	"employee_advance": "Employee advance",
	"security_deposit": "Security deposit",
	"other":            "Other",
}

// recoverableLabel is the name of a seeded recoverable category, or the code.
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

// amountValue formats paise for a money input box, without the ₹ sign (the
// box shows its own). Zero or less gives an empty box.
func amountValue(paise int64) string {
	if paise <= 0 {
		return ""
	}
	return strings.TrimPrefix(money.FormatPaise(paise), "₹")
}

// threadDot is the colour class of a line's dot in the request conversation
// (the thread on the request page).
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

// threadGlyph is the symbol in a conversation line's dot. A comment shows its
// author's initials and an attachment the upload symbol, whatever the action.
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

// threadValue formats one old or new value of a changed field. The audit
// stores raw JSON, so amounts (in paise, possibly like 2.35e+07) are formatted
// as money and the urgent flag as Urgent/Normal; empty values show "—".
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

// threadField is the label for a changed column, falling back to the column
// name with spaces for underscores.
func threadField(column string) string {
	if label, ok := threadFieldLabels[column]; ok {
		return label
	}
	return strings.ReplaceAll(column, "_", " ")
}

// threadFieldLabels are the labels threadField knows.
var threadFieldLabels = map[string]string{
	"amount": "Amount", "approved_amount": "Approved amount", "needed_by": "Needed by",
	"invoice_no": "Invoice number", "invoice_date": "Invoice date", "expense_date": "Expense date",
	"manager_id": "Approver", "short_title": "Short title", "purpose": "Purpose",
	"urgent": "Urgency",
}

// fileKind is the file-type tag shown on a document icon: the file extension
// in capitals, at most four letters, or "FILE".
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

// deref returns *p, or 0 for nil.
func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// activeHold reports whether req is on hold in effect: the flag is set and the
// request is still approved. (Leaving "approved" clears the flag, but older
// rows may still carry it.) The pill, the waiting line and the controls all
// ask this rather than reading the flag.
func activeHold(req store.Request) bool {
	return req.OnHold && req.Status == "approved"
}

// approvedOf is the amount payments are measured against: the approved amount,
// or the requested amount if none was recorded.
func approvedOf(req store.Request) int64 {
	if req.ApprovedAmount != nil {
		return *req.ApprovedAmount
	}
	return req.Amount
}

// subPaise is a - b, for templates (the "sub" helper).
func subPaise(a, b int64) int64 { return a - b }

// hhmm is t's local clock time ("14:02"), or "" for nil.
func hhmm(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format("15:04")
}

// since is how long ago t was: "just now", "N m" under an hour, else "N h".
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

// reservedLabel describes when a reservation was made: the clock time while it
// is younger than store.StaleReservation, and the elapsed hours after that.
func reservedLabel(t *time.Time) string {
	if t == nil {
		return ""
	}
	if time.Since(*t) < store.StaleReservation {
		return hhmm(t)
	}
	return since(t)
}

// stale reports whether a reservation made at t is older than
// store.StaleReservation, so the queue can link that row to the "reserved too
// long" screen.
func stale(t *time.Time) bool {
	return t != nil && time.Since(*t) >= store.StaleReservation
}

// trailAction is the action name used to draw an audit entry on a merged
// request-and-payment trail. A payment's "create", "update" and "attach" mean
// something different from the same words on a request, so they become
// record_payment, amend_payment and attach_payment (names no store writer
// uses). Everything else is unchanged.
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

// trailBody is an audit entry's summary as printed on a trail, below a heading
// that already names the actor. Request summaries start with the actor's name
// ("Priya Nair approved …"), so that name is removed and the next letter
// capitalised; summaries that do not start with it are left as they are. Any
// note on the entry is appended.
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

// initials is the first letters of the first two words of name, in capitals
// ("··" for an empty name); a comment's dot shows it.
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
