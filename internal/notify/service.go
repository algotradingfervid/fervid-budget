// This file is the notification service itself: for one event on one request
// it works out who should be told, writes their in-app notifications, and
// sends the email when the admin has switched email on for that event. It also
// renders the admin-editable {{token}} templates used for both.

package notify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// Service sends workflow notifications. The application builds one at
// start-up and shares it with the reminder job, so both use the same mailer.
type Service struct {
	st     *store.Store
	mailer Mailer
}

// NewService builds a Service that reads rules and people from st and sends
// email through mailer.
func NewService(st *store.Store, mailer Mailer) *Service {
	return &Service{st: st, mailer: mailer}
}

// RequestView is the flattened request an email template renders from. It
// carries display strings, not store types, so a template can never reach into
// the model; templateFields decides which of these a template may use.
type RequestView struct {
	Number, Treatment, Type, Project, Head, Payee, Purpose, Status string
	Amount, ApprovedAmount                                         int64
	NeededBy                                                       string
	RequesterName, RequesterEmail                                  string
	ManagerName, ManagerEmail                                      string
	SubmittedOn, ProcessingOn                                      string
	// PaidAmount and PaidOn describe the payment linked to the request, and
	// are zero until one exists. They are what the settlement events mean by
	// "paid": Amount is what was asked for, ApprovedAmount what was signed
	// off, and neither is what left the bank.
	PaidAmount int64
	PaidOn     string
	Link       string
	// HolderEmail is the accountant holding the reservation (processing_by),
	// empty when nobody holds it or the holder may not see the request. It is
	// routing data, not a template token: the events routesToHolder names are
	// emailed to this one address instead of the whole Accounts group.
	HolderEmail string
}

// Notify writes the in-app rows first — they always fire — and then, only if
// the event's email channel is switched on, resolves email recipients and
// sends. A failure to send email never costs the user their in-app record.
func (s *Service) Notify(ctx context.Context, event string, req store.Request) error {
	_, err := s.notify(ctx, event, req)
	return err
}

// notify is Notify that also returns the names of the people who got an
// in-app notification. The reminder job records those names on the request's
// history; taking them from the delivery itself, rather than resolving the
// recipients a second time, keeps the history and the delivery in step.
//
// Everyone is checked against the request's visibility rules first: Accounts
// users who may not see the request are dropped, and the requester's,
// approver's or holder's email address is blanked if that person may not see
// it. An unknown event (no rule row) is silently ignored.
func (s *Service) notify(ctx context.Context, event string, req store.Request) ([]string, error) {
	cfg, err := s.st.NotificationSetting(ctx, event)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil // unknown event: nothing configured, nothing to deliver
	}
	if err != nil {
		return nil, err
	}
	app, err := s.st.GetMailSettings(ctx)
	if err != nil {
		return nil, err
	}
	view, err := s.newRequestView(ctx, req, app)
	if err != nil {
		return nil, err
	}
	accountUsers, err := s.accountsUsers(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var visibleAccounts []store.User
	for _, u := range accountUsers {
		if s.st.UserCanViewRequest(ctx, u.ID, req) {
			visibleAccounts = append(visibleAccounts, u)
		}
	}
	accountUsers = visibleAccounts
	if !s.st.UserCanViewRequest(ctx, req.RequesterID, req) {
		view.RequesterEmail = ""
	}
	if !s.st.UserCanViewRequest(ctx, req.ManagerID, req) {
		view.ManagerEmail = ""
	}

	if req.ProcessingBy != nil && !s.st.UserCanViewRequest(ctx, *req.ProcessingBy, req) {
		view.HolderEmail = ""
	}
	// 1. In-app, always.
	told, err := s.writeInApp(ctx, event, cfg, req, view, accountUsers)
	if err != nil {
		return told, err
	}

	// 2. Email, only when this event opted in.
	if !cfg.EmailEnabled {
		return told, nil
	}
	to, cc := resolveRecipients(event, cfg, app, view, emails(accountUsers))
	if len(to) == 0 && len(cc) == 0 {
		return told, nil
	}
	subject, err := renderTemplate(cfg.SubjectTemplate, view)
	if err != nil {
		return told, fmt.Errorf("notification %q subject: %w", event, err)
	}
	body, err := renderTemplate(cfg.BodyTemplate, view)
	if err != nil {
		return told, fmt.Errorf("notification %q body: %w", event, err)
	}
	return told, s.mailer.Send(ctx, Message{From: formatFrom(app), To: to, Cc: cc, Subject: subject, Body: body})
}

// accountsUsers loads the Accounts group (everyone who can process payments)
// when the rule includes Accounts, and nobody otherwise.
func (s *Service) accountsUsers(ctx context.Context, cfg store.NotificationSetting) ([]store.User, error) {
	if !cfg.IncludeAccounts {
		return nil, nil
	}
	return s.st.UsersWithPermission(ctx, "payment", "process")
}

// writeInApp creates one notifications row per resolved recipient who may see
// the request, and returns those recipients' names so the reminder job can
// record who it told. The title is the rendered subject when the template is
// valid and a plain fallback when it is not — a broken email template must
// never cost the user their in-app record.
//
// Kind is left empty: store.AddNotification derives it from the event, which is
// the one definition the notification centre's filter queries.
func (s *Service) writeInApp(ctx context.Context, event string, cfg store.NotificationSetting, req store.Request, v RequestView, accountUsers []store.User) ([]string, error) {
	var accountIDs []int64
	names := map[int64]string{}
	for _, u := range accountUsers {
		accountIDs = append(accountIDs, u.ID)
		names[u.ID] = u.Name
	}
	if req.RequesterID != 0 && v.RequesterName != "" {
		names[req.RequesterID] = v.RequesterName
	}
	if req.ManagerID != 0 && v.ManagerName != "" {
		names[req.ManagerID] = v.ManagerName
	}
	title, err := renderTemplate(cfg.SubjectTemplate, v)
	if err != nil || strings.TrimSpace(title) == "" {
		title = cfg.Label
		if title == "" {
			title = event
		}
		title += " · " + v.Number
	}
	body, err := renderInAppBody(cfg.BodyTemplate, v)
	if err != nil {
		body = ""
	}
	var reqID *int64
	if req.ID != 0 {
		id := req.ID
		reqID = &id
	}
	var told []string
	for _, uid := range resolveInAppUsers(event, cfg, req, accountIDs) {
		if !s.st.UserCanViewRequest(ctx, uid, req) {
			continue
		}
		if _, err := s.st.AddNotification(ctx, store.Notification{
			UserID: uid, Event: event, RequestID: reqID,
			Title: title, Body: body, Href: requestHref(req),
		}); err != nil {
			return told, err
		}
		name, ok := names[uid]
		if !ok {
			// The reservation holder addressed personally need not still hold
			// payment:process — a reservation outlives a role change — so their
			// name is not always in the group already loaded.
			//
			// A failure here costs a name in an audit summary, never a delivery:
			// the row above is already written, and returning an error would
			// abort the reminder batch before MarkReminderSent stamps the
			// cadence, which would re-send the same reminder on every tick.
			if u, uerr := s.st.UserByID(ctx, uid); uerr == nil {
				name = u.Name
				names[uid] = name
			}
		}
		if name != "" {
			told = append(told, name)
		}
	}
	return told, nil
}

// audience is which of a rule's recipient groups are told about one event on
// one request. Both channels (in-app and email) start from it, so they cannot
// disagree about who an event is for.
type audience struct {
	requester bool
	manager   bool // the request's approver
	// accounts means the Accounts side: the reservation holder for the events
	// routesToHolder names, otherwise the whole Accounts group.
	accounts bool
	// copyManagement adds the management addresses from the mail settings to
	// the email's Cc line. It has no in-app counterpart.
	copyManagement bool
}

// audienceFor applies a rule's include switches to one event. The only event
// with extra conditions is the urgent flag, which has two send points: while
// the request is still pending it is the approver's problem, and once it is
// past approval it is Accounts' (with management copied on the email) and no
// longer the approver's.
func audienceFor(event string, cfg store.NotificationSetting, status string) audience {
	urgentBeforeApproval := event == EventRequestUrgent && status == "pending"
	urgentAfterApproval := event == EventRequestUrgent && status != "pending"
	return audience{
		requester:      cfg.IncludeRequester,
		manager:        cfg.IncludeManager && !urgentAfterApproval,
		accounts:       cfg.IncludeAccounts && !urgentBeforeApproval,
		copyManagement: event == EventRequestApproved || urgentAfterApproval,
	}
}

// resolveInAppUsers lists the user ids that get an in-app notification, in
// order and without duplicates. For the events routesToHolder names, the
// Accounts side is the user holding the reservation whenever somebody holds
// it, even if that user turns out not to be allowed to see the request (they
// are then skipped, and the group is not told instead).
func resolveInAppUsers(event string, cfg store.NotificationSetting, req store.Request, accountIDs []int64) []int64 {
	who := audienceFor(event, cfg, req.Status)
	var ids []int64
	if who.requester && req.RequesterID != 0 {
		ids = append(ids, req.RequesterID)
	}
	if who.manager && req.ManagerID != 0 {
		ids = append(ids, req.ManagerID)
	}
	if who.accounts {
		if routesToHolder(event) && req.ProcessingBy != nil && *req.ProcessingBy != 0 {
			ids = append(ids, *req.ProcessingBy) // the person who reserved it
		} else {
			ids = append(ids, accountIDs...)
		}
	}
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// resolveRecipients works out the email To and Cc lists: the rule's extra
// addresses plus the requester's, approver's and Accounts' addresses its
// audience calls for, lower-cased and without duplicates (an address on To is
// not repeated on Cc). For the events routesToHolder names, Accounts means the
// holder's address when there is one; unlike the in-app side, a request whose
// holder has no usable address (none held, no email, or not allowed to see
// the request) falls back to the whole Accounts group.
func resolveRecipients(event string, cfg store.NotificationSetting, app store.MailSettings, v RequestView, accounts []string) (to, cc []string) {
	who := audienceFor(event, cfg, v.Status)
	toList := splitList(cfg.ToRecipients)
	if who.requester {
		toList = append(toList, v.RequesterEmail)
	}
	if who.manager {
		toList = append(toList, v.ManagerEmail)
	}
	if who.accounts {
		if routesToHolder(event) && v.HolderEmail != "" {
			toList = append(toList, v.HolderEmail)
		} else {
			toList = append(toList, accounts...)
		}
	}
	ccList := splitList(cfg.CcRecipients)
	if who.copyManagement {
		ccList = append(ccList, splitList(app.ManagementRecipients)...)
	}
	to = dedupe(nil, toList)
	cc = dedupe(to, ccList)
	return to, cc
}

// routesToHolder reports whether the Accounts half of an event's audience is
// the accountant holding the reservation rather than the whole group. The
// in-app and the email resolvers both ask it, so the two channels agree about
// who "the assigned accountant" is.
func routesToHolder(event string) bool {
	switch event {
	// Events about one specific reservation. Addressing the whole Accounts
	// group would chase several people about work that belongs to one, and for
	// reservation_reassigned the single right recipient is the *new* holder —
	// ReassignReservation moves processing_by, so by the time this fires the
	// column already names them.
	//
	// A partial review and a cancellation both keep processing_by set (the
	// request never left 'processing' before it got there), so the accountant
	// who recorded the shortfall or froze the payment is the one told. The
	// fallback to the whole group is what covers a request whose holder let
	// it go — reservation_released clears processing_by, and a request that
	// reached partial_review or cancellation_requested some other way has
	// nobody personally on the hook.
	case EventCancellationRequested, EventReminderStaleReservation,
		EventReservationReassigned, EventPaymentPartialAccepted, EventPaymentPartialConcern,
		EventCancellationAccepted, EventCancellationDeclined:
		return true
	}
	return false
}

// requestHref is the site-relative link to a request's page, or to the
// request list when the request has no id.
func requestHref(req store.Request) string {
	if req.ID == 0 {
		return "/requests"
	}
	return fmt.Sprintf("/requests/%d", req.ID)
}

// newRequestView loads what the templates need about a request: the names and
// emails of its requester, approver and reservation holder, its dates, and its
// linked payment if any. A person or payment that no longer exists leaves its
// fields empty rather than failing; any other database error is returned.
func (s *Service) newRequestView(ctx context.Context, req store.Request, app store.MailSettings) (RequestView, error) {
	requester, err := s.st.UserByID(ctx, req.RequesterID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return RequestView{}, err
	}
	manager, err := s.st.UserByID(ctx, req.ManagerID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return RequestView{}, err
	}
	v := RequestView{
		Number: req.Number, Treatment: req.Treatment, Type: req.Type,
		Project: req.Project, Head: req.Head, Payee: req.Vendor,
		Purpose: req.Purpose, Status: req.Status, Amount: req.Amount, NeededBy: req.NeededBy,
		RequesterName: requester.Name, RequesterEmail: requester.Email,
		ManagerName: manager.Name, ManagerEmail: manager.Email,
		Link: app.BaseURL + requestHref(req),
	}
	if req.ApprovedAmount != nil {
		v.ApprovedAmount = *req.ApprovedAmount
	}
	if req.ProcessingBy != nil && *req.ProcessingBy != 0 {
		holder, err := s.st.UserByID(ctx, *req.ProcessingBy)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return RequestView{}, err
		}
		v.HolderEmail = holder.Email
	}
	if req.SubmittedAt != nil {
		v.SubmittedOn = req.SubmittedAt.UTC().Format("2006-01-02")
	}
	if req.ProcessingAt != nil {
		v.ProcessingOn = req.ProcessingAt.UTC().Format("2006-01-02")
	}
	// No payment yet is the ordinary case for most events, so ErrNotFound is
	// an answer here, not a failure: the two paid fields simply stay empty.
	pay, err := s.st.PaymentForRequest(ctx, req.ID)
	switch {
	case err == nil:
		// Settlement messages describe the request total, including every installment.
		v.PaidAmount, v.PaidOn = req.PaidAmount, pay.PaidOn
	case !errors.Is(err, store.ErrNotFound):
		return RequestView{}, err
	}
	return v, nil
}

// tokenPattern finds {{token}} placeholders in admin-written text.
var tokenPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// templateFields is the one list of {{tokens}} an admin may use in a
// notification's subject or body, in the order the admin screen's hint shows
// them, with how each is filled from a RequestView. Adding a token here makes
// it both usable and listed.
var templateFields = []struct {
	name  string
	value func(RequestView) string
}{
	{"number", func(v RequestView) string { return v.Number }},
	{"amount", func(v RequestView) string { return money.FormatPaise(v.Amount) }},
	{"approved_amount", func(v RequestView) string { return money.FormatPaise(v.ApprovedAmount) }},
	{"payee", func(v RequestView) string { return v.Payee }},
	{"requester", func(v RequestView) string { return v.RequesterName }},
	{"approver", func(v RequestView) string { return v.ManagerName }},
	{"project", func(v RequestView) string { return v.Project }},
	{"head", func(v RequestView) string { return v.Head }},
	{"purpose", func(v RequestView) string { return v.Purpose }},
	{"status", func(v RequestView) string { return v.Status }},
	{"needed_by", func(v RequestView) string { return v.NeededBy }},
	{"submitted_on", func(v RequestView) string { return v.SubmittedOn }},
	{"processing_on", func(v RequestView) string { return v.ProcessingOn }},
	{"paid_amount", func(v RequestView) string { return money.FormatPaise(v.PaidAmount) }},
	{"paid_on", func(v RequestView) string { return v.PaidOn }},
	{"link", func(v RequestView) string { return v.Link }},
}

// notifyFields fills every template token from v, keyed by token name.
func notifyFields(v RequestView) map[string]string {
	fields := make(map[string]string, len(templateFields))
	for _, f := range templateFields {
		fields[f.name] = f.value(v)
	}
	return fields
}

// NotifyFieldNames lists the {{tokens}} an admin may use, in the order the
// notification rules screen shows them in its hint.
func NotifyFieldNames() []string {
	names := make([]string, len(templateFields))
	for i, f := range templateFields {
		names[i] = f.name
	}
	return names
}

// renderTemplate substitutes {{token}} placeholders. It deliberately does NOT
// execute Go templates: notification text is admin-editable, and an editable
// string that reaches text/template is an execution surface. An unrecognised
// token is an error, so a typo fails on save instead of silently emitting an
// empty string into an email nobody can debug.
func renderTemplate(tmpl string, v RequestView) (string, error) {
	fields := notifyFields(v)
	var bad []string
	out := tokenPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		val, ok := fields[key]
		if !ok {
			bad = append(bad, key)
			return match
		}
		return val
	})
	if len(bad) > 0 {
		return "", fmt.Errorf("unknown template field(s): %s", strings.Join(bad, ", "))
	}
	return out, nil
}

// renderInAppBody renders the body template for the in-app notification
// centre, which is not paper: a line whose field came out empty is dropped
// instead of leaving a bare label ("Needed by:"), and the blank lines that
// leaves behind collapse to one paragraph break. Line breaks are kept; the
// centre shows them. A line of admin text with no token is always kept.
func renderInAppBody(tmpl string, v RequestView) (string, error) {
	fields := notifyFields(v)
	var lines []string
	for _, line := range strings.Split(tmpl, "\n") {
		empty := false
		for _, m := range tokenPattern.FindAllStringSubmatch(line, -1) {
			if val, ok := fields[strings.TrimSpace(m[1])]; ok && strings.TrimSpace(val) == "" {
				empty = true
			}
		}
		if empty {
			continue
		}
		out, err := renderTemplate(line, v)
		if err != nil {
			return "", err
		}
		out = strings.TrimRight(out, " \t\r")
		if out == "" && (len(lines) == 0 || lines[len(lines)-1] == "") {
			continue // no leading or doubled blank lines
		}
		lines = append(lines, out)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n"), nil
}

// ValidateTemplate reports whether admin-entered text uses only known tokens.
// The rules screen calls it on save so a typo is caught there.
func ValidateTemplate(tmpl string) error {
	_, err := renderTemplate(tmpl, RequestView{})
	return err
}

// splitList splits an admin-typed address list on commas, semicolons or new
// lines, lower-cases each address and drops empty ones.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if e := strings.ToLower(strings.TrimSpace(part)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// dedupe returns the non-empty addresses of in, lower-cased, in order, without
// repeats and without any address already in alreadySeen.
func dedupe(alreadySeen, in []string) []string {
	seen := map[string]bool{}
	for _, e := range alreadySeen {
		seen[e] = true
	}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// emails returns the users' non-empty email addresses.
func emails(users []store.User) []string {
	var out []string
	for _, u := range users {
		if u.Email != "" {
			out = append(out, u.Email)
		}
	}
	return out
}

// formatFrom builds the From header, "Name <address>" when both are set in
// the mail settings, otherwise just the address.
func formatFrom(app store.MailSettings) string {
	name := strings.TrimSpace(app.SMTPFromName)
	addr := strings.TrimSpace(app.SMTPFromAddr)
	if name != "" && addr != "" {
		return fmt.Sprintf("%s <%s>", name, addr)
	}
	return addr
}
