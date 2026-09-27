// request_list.go answers "which requests" questions: the filtered, paged and
// counted request lists behind the list screens, queues, dashboard panels and
// CSV export, plus the similar-request and duplicate-invoice lookups shown on
// the request form. Every list applies the viewer's request scope in SQL.
package store

import (
	"context"
	"fmt"
	"strings"
)

// requestBuckets are the status groups behind the list screen's tabs. The
// "needs-me" tab depends on the viewer and is built in requestWhere instead.
var requestBuckets = map[string][]string{
	"open":   {"pending", "returned", "approved", "cancellation_requested", "processing", "partial_review"},
	"closed": {"rejected", "withdrawn", "cancelled", "completed", "completed_partial"},
}

// requestWhere turns the list options into a WHERE clause. The scope comes from
// requestScopeSQL, the one SQL spelling of who may see a request; it fails
// closed, so an empty or unrecognised scope reads no rows. The work-queue
// filters (RoutedToViewer, HeldByViewer) are layered on top of the scope and
// never replace it: a queue must not list a request whose detail page the same
// person is refused.
func requestWhere(opts RequestListOptions) (string, []any) {
	var where []string
	var args []any
	scopeSQL, scopeArgs := requestScopeSQL("r", opts.Scope, opts.ViewerID)
	where = append(where, scopeSQL)
	args = append(args, scopeArgs...)
	if opts.RoutedToViewer {
		where = append(where, `r.manager_id=?`)
		args = append(args, opts.ViewerID)
	}
	if opts.HeldByViewer {
		where = append(where, `r.processing_by=?`)
		args = append(args, opts.ViewerID)
	}
	if opts.ConcernOpen {
		where = append(where, `r.concern_open=1`)
	}
	placeholders := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("?,", n), ",")
	}
	switch {
	case len(opts.Statuses) > 0:
		where = append(where, `r.status IN (`+placeholders(len(opts.Statuses))+`)`)
		for _, st := range opts.Statuses {
			args = append(args, st)
		}
	case opts.Bucket == "needs-me":
		// Requests where the viewer owes the next action: their own returned
		// requests; requests they must decide as approver (including partial
		// reviews); and partial reviews they hold as the accountant while a
		// concern is waiting for their answer.
		where = append(where, `((r.requester_id=? AND r.status='returned')
 OR (r.manager_id=? AND r.status IN ('pending','cancellation_requested','partial_review'))
 OR (r.processing_by=? AND r.status='partial_review' AND r.concern_open=1))`)
		args = append(args, opts.ViewerID, opts.ViewerID, opts.ViewerID)
	case opts.Bucket != "" && opts.Bucket != "all":
		statuses := requestBuckets[opts.Bucket]
		if len(statuses) == 0 {
			statuses = []string{opts.Bucket}
		}
		where = append(where, `r.status IN (`+placeholders(len(statuses))+`)`)
		for _, st := range statuses {
			args = append(args, st)
		}
	case opts.Status != "" && opts.Status != "all":
		where = append(where, `r.status=?`)
		args = append(args, opts.Status)
	}
	if opts.Type != "" {
		where = append(where, `r.type=?`)
		args = append(args, opts.Type)
	}
	if opts.Treatment != "" {
		where = append(where, `r.treatment=?`)
		args = append(args, opts.Treatment)
	}
	if opts.VendorID > 0 {
		where = append(where, `r.vendor_id=?`)
		args = append(args, opts.VendorID)
	}
	if opts.ProjectID > 0 {
		where = append(where, `r.project_id=?`)
		args = append(args, opts.ProjectID)
	}
	if q := escapeLike(opts.Query); q != "" {
		where = append(where, `(lower(r.number) LIKE ? ESCAPE '\' OR lower(r.purpose) LIKE ? ESCAPE '\'
 OR lower(r.short_title) LIKE ? ESCAPE '\' OR lower(r.invoice_no) LIKE ? ESCAPE '\'
 OR lower(COALESCE(v.name,r.vendor_payee)) LIKE ? ESCAPE '\' OR lower(ru.name) LIKE ? ESCAPE '\')`)
		needle := "%" + q + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	clause := ""
	if len(where) > 0 {
		clause = ` WHERE ` + strings.Join(where, ` AND `)
	}
	return clause, args
}

// RequestsUnlimited asks for every row the filter matches, with no cap at all.
// The CSV export uses it: an export that silently dropped rows would be worse
// than none.
const RequestsUnlimited = -1

// defaultRequestLimit is the cap applied when a caller names no number. It is a
// guard against a panel accidentally reading the whole table, not a display
// decision — a screen that lists requests uses ListRequestsPage, which reports
// the total and whether anything was left out.
const defaultRequestLimit = 200

// RequestPage is one page of requests plus the total the filter matches, so a
// screen can say "showing 200 of 214" and offer the next page instead of
// silently dropping rows.
type RequestPage struct {
	Requests []Request
	// Total rows matching the same filter, ignoring Limit and Offset. This is
	// exactly CountRequests, so a tab built from Total can never disagree with
	// the rows beneath it.
	Total int
	// Offset and Limit as they were applied. Limit is 0 when no cap was applied.
	Offset int
	Limit  int
	// Truncated reports that rows matching the filter are not in Requests —
	// either later pages, or rows the cap dropped.
	Truncated bool
}

// RequestPageOptions is RequestListOptions plus the offset a paged screen needs.
// Offset lives here rather than on RequestListOptions because every existing
// caller of that struct means "the first N", and its zero value must go on
// meaning exactly that.
type RequestPageOptions struct {
	RequestListOptions
	Offset int
}

// ListRequestsPage returns a window onto the matching requests together with the
// total the same filter matches, so the screen can say what it is not showing.
func (s *Store) ListRequestsPage(ctx context.Context, opts RequestPageOptions) (RequestPage, error) {
	total, err := s.CountRequests(ctx, opts.RequestListOptions)
	if err != nil {
		return RequestPage{}, err
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	rows, err := s.listRequestRows(ctx, opts.RequestListOptions, opts.Offset)
	if err != nil {
		return RequestPage{}, err
	}
	page := RequestPage{Requests: rows, Total: total, Offset: opts.Offset}
	if opts.Limit != RequestsUnlimited {
		page.Limit = effectiveRequestLimit(opts.Limit)
	}
	page.Truncated = opts.Offset+len(rows) < total
	return page, nil
}

// ListRequests returns the matching rows and nothing about what it left out. A
// caller that renders a list, or exports one, wants ListRequestsPage instead;
// this stays for the panels that deliberately show only a head (the dashboard's
// four rows) and for the queues that pass an explicit Limit.
func (s *Store) ListRequests(ctx context.Context, opts RequestListOptions) ([]Request, error) {
	return s.listRequestRows(ctx, opts, 0)
}

// effectiveRequestLimit is the row cap for a requested limit: the limit itself,
// or defaultRequestLimit when none was given.
func effectiveRequestLimit(limit int) int {
	if limit <= 0 {
		return defaultRequestLimit
	}
	return limit
}

// listRequestRows runs the list query for opts, starting offset rows in and
// capped at the effective limit (or uncapped for RequestsUnlimited).
func (s *Store) listRequestRows(ctx context.Context, opts RequestListOptions, offset int) ([]Request, error) {
	clause, args := requestWhere(opts)
	// Urgent first, then oldest first, so the request kept waiting longest is
	// at the top. Timestamps are only to the second, so id breaks ties in
	// arrival order.
	q := requestSelect + clause + ` ORDER BY r.urgent DESC, r.created_at, r.id`
	switch {
	case opts.Limit == RequestsUnlimited && offset > 0:
		// SQLite has no bare OFFSET: -1 is its "no limit" sentinel.
		q += ` LIMIT -1 OFFSET ?`
		args = append(args, offset)
	case opts.Limit == RequestsUnlimited:
	case offset > 0:
		q += ` LIMIT ? OFFSET ?`
		args = append(args, effectiveRequestLimit(opts.Limit), offset)
	default:
		q += ` LIMIT ?`
		args = append(args, effectiveRequestLimit(opts.Limit))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountRequests counts the requests the filter matches, ignoring Limit.
func (s *Store) CountRequests(ctx context.Context, opts RequestListOptions) (int, error) {
	clause, args := requestWhere(opts)
	q := `SELECT COUNT(*) FROM payment_requests r
 JOIN users ru ON ru.id=r.requester_id
 LEFT JOIN vendors v ON v.id=r.vendor_id` + clause
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// SimilarRequests reports recent requests that look like the one being raised:
// the same payee, and either a near-identical amount or the very same invoice
// reference, inside the configured window. It is a warning, not a gate — callers
// must never refuse a submit on the strength of a non-empty result.
func (s *Store) SimilarRequests(ctx context.Context, opt SimilarRequestOptions) ([]Request, error) {
	if opt.Days <= 0 {
		opt.Days = 30
	}
	if opt.Limit <= 0 {
		opt.Limit = 5
	}
	payee := strings.ToLower(strings.TrimSpace(opt.Payee))
	if opt.VendorID <= 0 && payee == "" {
		return nil, nil
	}
	// ±1% of the amount, so "₹1,00,000 again" is caught but "₹2,500" is not.
	tolerance := opt.Amount / 100
	if tolerance < 100 {
		tolerance = 100
	}
	invoice := strings.ToLower(strings.TrimSpace(opt.InvoiceNo))
	q := requestSelect + ` WHERE r.id<>?
 AND r.status NOT IN ('withdrawn','rejected','cancelled')
 AND r.created_at >= datetime('now', ?)
 AND (( ? > 0 AND r.vendor_id = ? ) OR ( ? <> '' AND lower(COALESCE(v.name, r.vendor_payee)) = ? ))
 AND (( ? > 0 AND abs(r.amount - ?) <= ? ) OR ( ? <> '' AND lower(r.invoice_no) = ? ))
 ORDER BY r.created_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q,
		opt.ExcludeID,
		fmt.Sprintf("-%d days", opt.Days),
		opt.VendorID, opt.VendorID,
		payee, payee,
		opt.Amount, opt.Amount, tolerance,
		invoice, invoice,
		opt.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DuplicateInvoices lists the live vendor invoices that match in (same vendor,
// same invoice number ignoring case and surrounding spaces, same invoice year), limited to
// the requests the viewer may see under opts. The write guard
// (requireDuplicateReasonTx) still counts matches outside their scope, without
// revealing them.
func (s *Store) DuplicateInvoices(ctx context.Context, in RequestInput, opts RequestListOptions) ([]Request, error) {
	opts.Type = "vendor_invoice"
	opts.Bucket = "all"
	opts.Limit = RequestsUnlimited
	all, err := s.ListRequests(ctx, opts)
	if err != nil {
		return nil, err
	}
	var out []Request
	for _, r := range all {
		if r.VendorID != nil && *r.VendorID == in.VendorID && strings.EqualFold(strings.TrimSpace(r.InvoiceNo), strings.TrimSpace(in.InvoiceNo)) && len(r.InvoiceDate) >= 4 && len(in.InvoiceDate) >= 4 && r.InvoiceDate[:4] == in.InvoiceDate[:4] && r.Status != "withdrawn" && r.Status != "rejected" && r.Status != "cancelled" {
			out = append(out, r)
		}
	}
	return out, nil
}
