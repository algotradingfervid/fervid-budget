package app

// The audit log screen: filtering, paging, hiding rows the reader may not
// see, and the labels for audit actions and entities.

import (
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func (a *App) auditLog(w http.ResponseWriter, r *http.Request) {
	// Every filter, the dates included, runs in SQL over the whole log and the
	// result is paged (audit-1). It used to read the newest 1000 rows, filter
	// action and actor in Go, and cut the list at 200 without a word, so older
	// rows could not be reached at all. from/to are local days, inclusive, as
	// the When column shows local time.
	qs := r.URL.Query()
	q := store.AuditQuery{
		EntityType: strings.TrimSpace(qs.Get("entity")),
		EntityID:   parseID(qs.Get("id")),
		Action:     strings.TrimSpace(qs.Get("action")),
		Actor:      qs.Get("actor"),
		Offset:     int(parseID(qs.Get("offset"))),
	}
	from, to := auditDay(qs.Get("from")), auditDay(qs.Get("to"))
	if !from.IsZero() {
		q.From = from
	}
	if !to.IsZero() {
		q.To = to.AddDate(0, 0, 1)
	}
	fetch := func(offset int) (store.AuditPage, error) {
		if a.auth.Scope(auth.CurrentUser(r), "request") == store.ScopeAll && a.auth.Scope(auth.CurrentUser(r), "payment") == store.ScopeAll && a.auth.Can(auth.CurrentUser(r), "request", "view") && a.auth.Can(auth.CurrentUser(r), "payment", "view") {
			q.Limit, q.Offset = auditPageSize, offset
			return a.st.AuditPage(r.Context(), q)
		}
		// A scoped reader's request rows are withheld in Go (below), so the
		// page is cut after that, over every matching row, to keep the count
		// and the pages honest. No seeded holder of audit:view takes this path.
		page, err := a.st.AuditPage(r.Context(), store.AuditQuery{EntityType: q.EntityType, EntityID: q.EntityID,
			Action: q.Action, Actor: q.Actor, From: q.From, To: q.To})
		if err != nil {
			return page, err
		}
		return pageAuditEntries(a.auditWithinRequestScope(r, page.Entries), offset), nil
	}
	page, err := fetch(q.Offset)
	// An offset past the end — a stale or hand-edited URL — used to show the
	// empty state as "Showing 0 of N" with a Newer link to a page that was
	// just as empty (audit-1 review). It lands on the last real page instead.
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
		data.AuditFrom = from.Format("2006-01-02")
	}
	if !to.IsZero() {
		data.AuditTo = to.Format("2006-01-02")
	}
	a.render(w, r, "audit", data)
}

// auditPageSize is how many audit rows one /audit page shows.
const auditPageSize = 100

// auditDay reads a from/to date as the start of that local day; anything else
// leaves that side of the range open.
func auditDay(v string) time.Time {
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(v), time.Local)
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
	end := offset + auditPageSize
	if end > len(entries) {
		end = len(entries)
	}
	page.Offset, page.Entries = offset, entries[offset:end]
	page.Truncated = end < len(entries)
	return page
}

// auditWithinRequestScope applies the request row scope to the audit log.
//
// The decision (F-G-017): the log is **scoped**, not redacted. C2 requires an
// audit history for request mutations, and a history whose evidence has been
// blanked out is not one — the whole value of `before_json`/`after_json` is that
// a reviewer can see what actually changed. But `before_json` for a
// payment_request row is the entire Request struct: number, purpose, requester,
// approver, amount, every date. Rendering that to a caller who is answered 403
// on /requests/{id} makes audit:view a way around Q5/R6, and it carries strictly
// more data than the screens that do check. So the rows themselves are withheld
// from a reader whose scope does not reach the request, exactly as
// loadViewableRequest withholds the request.
//
// Nothing else is touched: a payment, a budget or a user row is unaffected, and
// a caller with request=all — which is every seeded holder of audit:view — sees
// precisely what they saw before.
//
// The lookup is one query per distinct request id, memoised, and it runs only
// for a caller whose scope is narrower than "all". That is the same trade
// reassignCandidates makes; if it ever costs anything the fix is a batched read
// in the store, not a looser rule.
func (a *App) auditWithinRequestScope(r *http.Request, entries []store.AuditEntry) []store.AuditEntry {
	u := auth.CurrentUser(r)
	scope := a.auth.Scope(u, "request")
	if scope == store.ScopeAll && a.auth.Scope(u, "payment") == store.ScopeAll && a.auth.Can(u, "request", "view") && a.auth.Can(u, "payment", "view") {
		return entries
	}
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
		// A payment_request row with no id names no request, so there is no scope
		// to check it against and no way to prove it may be read.
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

// actionText spells an audit action for a person. It covers every action string
// the store writes, because an action with no case renders its raw lower-case
// identifier in the audit pill — which is what `settings` did on every
// configuration save (F-G-004). auditActions below is built from the same list,
// so the filter can never offer an action this cannot name.
func actionText(action string) string {
	switch action {
	case "recovery":
		return "Recovery recorded"
	case "create":
		return "Created"
	case "update", "edit":
		return "Updated"
	case "delete":
		return "Deleted"
	case "void":
		return "Voided"
	case "lock":
		return "Locked"
	case "unlock":
		return "Unlocked"
	case "login":
		return "Logged in"
	case "logout":
		return "Logged out"
	case "export":
		return "Exported"
	case "attach":
		return "Attached"
	case "login_failed":
		return "Login failed"
	case "settings":
		return "Settings saved"
	// The request workflow (Phase 2) and the settlement flow (Phase 3). Every one
	// of these is written against entity_type 'payment_request'.
	case "submit":
		return "Submitted"
	case "approve":
		return "Approved"
	case "return":
		return "Returned"
	case "reject":
		return "Rejected"
	case "withdraw":
		return "Withdrawn"
	case "reraise":
		return "Raised again"
	case "comment":
		return "Commented"
	case "cancel":
		return "Cancelled"
	case "cancel_request":
		return "Cancellation asked"
	case "approval_reassign":
		return "Approval reassigned"
	case "process":
		return "Reserved"
	case "reserve":
		return "Reserved"
	case "release":
		return "Released"
	case "reassign":
		return "Reservation reassigned"
	case "hold":
		return "Put on hold"
	case "unhold":
		return "Taken off hold"
	case "settle":
		return "Settled"
	case "mark_partial":
		return "Marked partial"
	case "accept_partial":
		return "Partial accepted"
	case "concern":
		return "Concern raised"
	case "remind":
		return "Reminder sent"
	case "view":
		return "Viewed"
	default:
		return action
	}
}

func actionClass(action string) string {
	switch action {
	case "void", "login_failed", "reject", "cancel", "concern":
		return "bad"
	case "lock", "unlock", "hold", "return", "withdraw", "mark_partial":
		return "warn"
	case "create", "submit", "approve", "settle", "accept_partial", "unhold":
		return "good"
	case "update", "edit", "settings", "comment", "attach", "process", "reserve",
		"release", "reassign", "approval_reassign", "reraise", "remind":
		return "info"
	default:
		return "neutral"
	}
}

// auditOption is one <option> of the audit screen's Entity and Action filters.
// Both lists live here rather than as literals in the template because the
// template's hand-maintained copy had drifted so far that `payment_request` —
// the entity every request, approval, reservation and settlement is filed under
// — could not be asked for at all, and no request action was offered either
// (F-G-004/F-C-06). Every value below is a string the store actually writes;
// `remind` arrived with migration v9's reminder audit row.
type auditOption struct {
	Value string
	Label string
}

// auditEntities are the entity_type values the store writes, in reading order:
// the request workflow first, then the ledger, then masters, then
// administration.
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

// auditActions are the action values the store writes, ordered so the request
// workflow reads as a workflow rather than as an alphabet.
func auditActions() []auditOption {
	values := []string{
		"create", "update", "delete", "attach", "comment",
		"submit", "approve", "return", "reject", "withdraw", "reraise",
		"cancel_request", "cancel", "approval_reassign",
		"process", "release", "reassign", "hold", "unhold",
		"settle", "mark_partial", "accept_partial", "concern", "remind",
		"recovery", "void", "lock", "unlock", "settings", "export",
		"login", "logout", "login_failed",
	}
	out := make([]auditOption, 0, len(values))
	for _, v := range values {
		out = append(out, auditOption{Value: v, Label: actionText(v)})
	}
	return out
}

func entityText(entity string) string {
	return strings.Title(strings.ReplaceAll(entity, "_", " "))
}
