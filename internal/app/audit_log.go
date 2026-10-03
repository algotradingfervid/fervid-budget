package app

// The audit log screen: filtering, paging, hiding rows the reader may not
// see, and the labels and filter options for audit actions and entities.

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// auditLog shows the audit log, newest first, one page of auditPageSize
// rows at a time. The filters (entity type and id, action, actor, and a
// from/to range of local days, both inclusive) all run in SQL over the whole
// log. An offset past the last page shows the last page instead.
func (a *App) auditLog(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := store.AuditQuery{
		EntityType: strings.TrimSpace(qs.Get("entity")),
		EntityID:   parseID(qs.Get("id")),
		Action:     strings.TrimSpace(qs.Get("action")),
		Actor:      qs.Get("actor"),
		Offset:     int(parseID(qs.Get("offset"))),
	}
	from, to := auditDay(qs.Get("from")), auditDay(qs.Get("to"))
	q.From = from
	if !to.IsZero() {
		q.To = to.AddDate(0, 0, 1) // up to the end of the "to" day
	}
	seesAll := a.seesWholeAudit(auth.CurrentUser(r))
	fetch := func(offset int) (store.AuditPage, error) {
		if seesAll {
			q.Limit, q.Offset = auditPageSize, offset
			return a.st.AuditPage(r.Context(), q)
		}
		// For a reader with a narrower scope, rows are filtered in Go, so every
		// matching row is read and the page is cut afterwards; that keeps the
		// total and the paging right.
		page, err := a.st.AuditPage(r.Context(), store.AuditQuery{EntityType: q.EntityType, EntityID: q.EntityID,
			Action: q.Action, Actor: q.Actor, From: q.From, To: q.To})
		if err != nil {
			return page, err
		}
		return pageAuditEntries(a.auditWithinRequestScope(r, page.Entries), offset), nil
	}
	page, err := fetch(q.Offset)
	if err == nil && len(page.Entries) == 0 && page.Total > 0 && q.Offset > 0 {
		page, err = fetch(lastAuditPageOffset(page.Total))
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data := PageData{Title: "Audit Log", Audit: page.Entries, AuditEntity: q.EntityType, AuditAction: q.Action, AuditActor: qs.Get("actor")}
	data.AuditPage, data.AuditID = page, qs.Get("id")
	if !from.IsZero() {
		data.AuditFrom = from.Format(time.DateOnly)
	}
	if !to.IsZero() {
		data.AuditTo = to.Format(time.DateOnly)
	}
	a.render(w, r, "audit", data)
}

// seesWholeAudit reports whether u may read every request and every payment,
// in which case no audit row needs hiding.
func (a *App) seesWholeAudit(u store.User) bool {
	return a.auth.Scope(u, "request") == store.ScopeAll && a.auth.Scope(u, "payment") == store.ScopeAll &&
		a.auth.Can(u, "request", "view") && a.auth.Can(u, "payment", "view")
}

// auditPageSize is how many audit rows one /audit page shows.
const auditPageSize = 100

// auditDay reads a from/to date as the start of that local day; anything else
// leaves that side of the range open.
func auditDay(v string) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(v), time.Local)
	if err != nil {
		return time.Time{}
	}
	return d
}

// lastAuditPageOffset is the offset of the last /audit page that still has a
// row on it; 0 when there are none.
func lastAuditPageOffset(total int) int {
	if total <= 0 {
		return 0
	}
	return (total - 1) / auditPageSize * auditPageSize
}

// pageAuditEntries cuts one /audit page out of an already-filtered list. An
// offset past the end is clamped to the last page with rows, as the SQL path
// in auditLog does.
func pageAuditEntries(entries []store.AuditEntry, offset int) store.AuditPage {
	page := store.AuditPage{Total: len(entries), Limit: auditPageSize}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(entries) {
		offset = lastAuditPageOffset(len(entries))
	}
	end := min(offset+auditPageSize, len(entries))
	page.Offset, page.Entries = offset, entries[offset:end]
	page.Truncated = end < len(entries)
	return page
}

// auditWithinRequestScope removes the audit rows the reader may not see. Rows
// are hidden, not blanked: an audit row about a request or payment carries
// the whole record (number, people, amounts), so showing it would reveal
// exactly what the record's own page refuses to show.
//
//   - A payment row stays when canReadPayment allows that payment.
//   - A request row stays when the reader has request:view and their request
//     scope reaches that request. A request row without an id is dropped.
//   - Every other row (budgets, users, settings …) always stays.
//
// A reader who sees everything gets the list back unchanged. Otherwise each
// distinct request is looked up once.
func (a *App) auditWithinRequestScope(r *http.Request, entries []store.AuditEntry) []store.AuditEntry {
	u := auth.CurrentUser(r)
	if a.seesWholeAudit(u) {
		return entries
	}
	scope := a.auth.Scope(u, "request")
	visible := map[int64]bool{}
	out := entries[:0]
	for _, entry := range entries {
		if entry.EntityType == "payment" {
			if entry.EntityID != nil {
				p, err := a.st.Payment(r.Context(), *entry.EntityID)
				if err == nil && a.canReadPayment(r, p) {
					out = append(out, entry)
				}
			}
			continue
		}
		if entry.EntityType != "payment_request" {
			out = append(out, entry)
			continue
		}
		if entry.EntityID == nil {
			continue
		}
		id := *entry.EntityID
		if _, known := visible[id]; !known {
			req, err := a.st.Request(r.Context(), id)
			visible[id] = err == nil && a.auth.Can(u, "request", "view") && store.RequestInScope(scope, u.ID, req)
		}
		if visible[id] {
			out = append(out, entry)
		}
	}
	return out
}

// auditActionInfo is how one audit action is shown. On the audit log it has
// a Label and the colour Class of its pill (an empty Label shows the action
// itself, an empty Class is "neutral"); InFilter says whether the Action
// filter offers it. On a history trail it has the colour Tone of its dot, the
// Glyph inside the dot and the verb Phrase printed after the actor's name
// ("Priya Nair reserved it for processing"); an empty field there falls back
// to no tone, the "·" glyph, or the audit-log label. The request conversation
// uses threadDot/threadGlyph instead, which differ for a few actions.
type auditActionInfo struct {
	Value, Label, Class string
	InFilter            bool
	Tone, Glyph, Phrase string
}

// auditActionTable lists every audit action the app knows, in the order the
// Action filter offers them (the request workflow reads as a workflow, not
// as an alphabet). After the filter's actions come older or rarely written
// names that still need a label, then the trail-only actions. The attach
// glyph is a text symbol rather than an emoji, so the CSS can tint it like
// the rest.
var auditActionTable = []auditActionInfo{
	{"create", "Created", "good", true, "", "＋", ""},
	{"update", "Updated", "info", true, "", "✎", "edited the request"},
	{"delete", "Deleted", "neutral", true, "", "", ""},
	{"attach", "Attached", "info", true, "", "⇪", "attached a document"},
	{"comment", "Commented", "info", true, "", "", "commented"},
	{"submit", "Submitted", "good", true, "brand", "＋", "submitted the request"},
	{"approve", "Approved", "good", true, "ok", "✓", "approved the request"},
	{"return", "Returned", "warn", true, "warn", "↩", "returned the request for correction"},
	{"reject", "Rejected", "bad", true, "bad", "✕", "rejected the request"},
	{"withdraw", "Withdrawn", "warn", true, "bad", "✕", "withdrew the request"},
	{"reraise", "Raised again", "info", true, "brand", "＋", "raised the request again"},
	{"cancel_request", "Cancellation asked", "neutral", true, "", "", "asked for the request to be cancelled"},
	{"cancel", "Cancelled", "bad", true, "bad", "✕", "cancelled the request"},
	{"approval_reassign", "Approval reassigned", "info", true, "brand", "✎", "reassigned the approval"},
	{"process", "Reserved", "info", true, "brand", "◷", "reserved it for processing"},
	{"release", "Released", "info", true, "warn", "↩", "released the reservation"},
	{"reassign", "Reservation reassigned", "info", true, "brand", "◷", "reassigned the reservation"},
	{"hold", "Put on hold", "warn", true, "warn", "⏸", "put the request on hold"},
	{"unhold", "Taken off hold", "good", true, "brand", "◷", "took the request off hold"}, // back in the takeable queue
	{"settle", "Settled", "good", true, "ok", "✓", "settled the request"},
	{"mark_partial", "Marked partial", "warn", true, "warn", "₹", "marked it a partial payment"},
	{"accept_partial", "Partial accepted", "good", true, "ok", "✓", "accepted the partial payment"},
	{"concern", "Concern raised", "bad", true, "warn", "₹", "raised a concern"},
	{"remind", "Reminder sent", "info", true, "", "", ""},
	{"recovery", "Recovery recorded", "neutral", true, "", "", ""},
	{"void", "Voided", "bad", true, "bad", "✕", "voided the payment"},
	{"lock", "Locked", "warn", true, "", "", ""},
	{"unlock", "Unlocked", "warn", true, "", "", ""},
	{"settings", "Settings saved", "info", true, "", "", ""},
	{"export", "Exported", "neutral", true, "", "", ""},
	{"login", "Logged in", "neutral", true, "", "", ""},
	{"logout", "Logged out", "neutral", true, "", "", ""},
	{"login_failed", "Login failed", "bad", true, "", "", ""},
	{"edit", "Updated", "info", false, "", "", ""},
	{"reserve", "Reserved", "info", false, "", "", ""},
	{"view", "Viewed", "neutral", false, "", "", ""},
	// Only history trails show these; the audit log shows them as they are.
	{"record_payment", "", "", false, "ok", "₹", "recorded a payment"},
	{"amend_payment", "", "", false, "", "✎", "corrected the payment"},
	{"attach_payment", "", "", false, "", "⇪", "attached proof of payment"},
}

// auditActionByValue indexes auditActionTable by action.
var auditActionByValue = func() map[string]auditActionInfo {
	m := make(map[string]auditActionInfo, len(auditActionTable))
	for _, info := range auditActionTable {
		m[info.Value] = info
	}
	return m
}()

// actionText is an audit action's label; an unknown action shows as itself.
func actionText(action string) string {
	if info := auditActionByValue[action]; info.Label != "" {
		return info.Label
	}
	return action
}

// actionClass is the colour class of an audit action's pill ("neutral" for
// an unknown action).
func actionClass(action string) string {
	if info := auditActionByValue[action]; info.Class != "" {
		return info.Class
	}
	return "neutral"
}

// auditTone is the colour class of an audit action's dot ("" for none).
func auditTone(action string) string { return auditActionByValue[action].Tone }

// auditGlyph is the symbol in an audit action's dot.
func auditGlyph(action string) string {
	if glyph := auditActionByValue[action].Glyph; glyph != "" {
		return glyph
	}
	return "·"
}

// auditPhrase is the verb phrase that follows the actor's name on a trail.
func auditPhrase(action string) string {
	if phrase := auditActionByValue[action].Phrase; phrase != "" {
		return phrase
	}
	return actionText(action)
}

// auditOption is one <option> of the audit screen's Entity or Action filter.
type auditOption struct {
	Value string
	Label string
}

// auditEntities are the entity types the store writes to the audit log, in
// reading order: the request workflow first, then the ledger, then masters,
// then administration.
func auditEntities() []auditOption {
	values := []string{
		"payment_request", "payment", "budget", "budget_month", "month_lock",
		"project", "head", "vendor", "recoverable_category", "recoverable_report",
		"user", "role", "notification_setting", "app_setting", "report", "backup",
	}
	out := make([]auditOption, 0, len(values))
	for _, v := range values {
		out = append(out, auditOption{Value: v, Label: entityText(v)})
	}
	return out
}

// auditActions are the Action filter's options, from auditActionTable.
func auditActions() []auditOption {
	var out []auditOption
	for _, info := range auditActionTable {
		if info.InFilter {
			out = append(out, auditOption{Value: info.Value, Label: info.Label})
		}
	}
	return out
}

// entityText turns an identifier such as "payment_request" into a label such
// as "Payment Request": underscores become spaces and every word starts with
// a capital letter.
func entityText(entity string) string {
	return titleWords(strings.ReplaceAll(entity, "_", " "))
}

// titleWords capitalises the first letter of every word. A word starts at
// the beginning of s and after any ASCII character other than a letter, digit
// or underscore, or after Unicode white space. This is the rule of the
// deprecated strings.Title, which this replaces.
func titleWords(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		startsWord := wordSeparator(prev)
		prev = r
		if startsWord {
			return unicode.ToTitle(r)
		}
		return r
	}, s)
}

// wordSeparator reports whether r ends a word for titleWords.
func wordSeparator(r rune) bool {
	if r <= unicode.MaxASCII {
		return !('0' <= r && r <= '9' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r == '_')
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return false
	}
	return unicode.IsSpace(r)
}
