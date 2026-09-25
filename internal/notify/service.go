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

type Service struct {
	st     *store.Store
	mailer Mailer
}

func NewService(st *store.Store, mailer Mailer) *Service {
	return &Service{st: st, mailer: mailer}
}

// RequestView is the flattened request an email template renders from. It
// carries display strings, not store types, so a template can never reach into
// the model and so the placeholder vocabulary has exactly one definition.
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
	// off, and neither is what left the bank (settlement-1).
	PaidAmount int64
	PaidOn     string
	Link       string
}

// Notify writes the in-app rows first — they always fire (G19) — and then, only
// if the event's email channel is switched on, resolves email recipients and
// sends. A failure to send email never costs the user their in-app record.
func (s *Service) Notify(ctx context.Context, event string, req store.Request) error {
	_, err := s.notify(ctx, event, req)
	return err
}

// notify is Notify with a report of who it reached. Only the reminder scheduler
// needs it: its audit row is the only witness the stale-reservation screen's
// "a reminder went out" claim has (F-F-02), and that row has to name who was
// told. Resolving the recipients a second time to write the row is exactly how
// the row and the delivery would drift apart, so the delivery reports itself.
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

	// 1. In-app, unconditionally (G19).
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

func (s *Service) accountsUsers(ctx context.Context, cfg store.NotificationSetting) ([]store.User, error) {
	if !cfg.IncludeAccounts {
		return nil, nil
	}
	return s.st.UsersWithPermission(ctx, "payment", "process")
}

// writeInApp creates one notifications row per resolved recipient and returns
// the recipients' names, which is what lets the reminder scheduler record who it
// told (F-F-02) without resolving them twice. The title is the rendered subject
// when the template is valid and a plain fallback when it is not — a broken
// email template must never cost the user their in-app record.
//
// Kind is left empty: store.AddNotification derives it from the event, which is
// the one definition the .segmented filter queries.
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
	body, err := renderTemplate(cfg.BodyTemplate, v)
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
		if _, err := s.st.AddNotification(ctx, store.Notification{
			UserID: uid, Event: event, RequestID: reqID,
			Title: title, Body: body, Href: requestHref(req),
		}); err != nil {
			return told, err
		}
		name, ok := names[uid]
		if !ok {
			// The assignee addressed personally below need not still hold
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

// resolveInAppUsers mirrors resolveRecipients in user-id space. The include
// flags are the same switches an admin sees; the assigned accountant is
// addressed personally for the events the design routes to them.
func resolveInAppUsers(event string, cfg store.NotificationSetting, req store.Request, accountIDs []int64) []int64 {
	var ids []int64
	if cfg.IncludeRequester && req.RequesterID != 0 {
		ids = append(ids, req.RequesterID)
	}
	if cfg.IncludeManager && req.ManagerID != 0 && !(event == EventRequestUrgent && req.Status != "pending") {
		ids = append(ids, req.ManagerID)
	}
	if cfg.IncludeAccounts && !(event == EventRequestUrgent && req.Status == "pending") {
		switch event {
		// Events about one specific reservation. Addressing the whole Accounts
		// group would chase four people about work that belongs to one, and for
		// reservation_reassigned the single right recipient is the *new* holder —
		// ReassignReservation moves processing_by, so by the time this fires the
		// column already names them.
		//
		// A partial review and a cancellation both keep processing_by set (the
		// request never left 'processing' before it got there), so the accountant
		// who recorded the shortfall or froze the payment is the one told. The
		// fallback to the whole group is what covers a request whose holder let
		// it go — reservation_released nulls processing_by, and a request that
		// reached partial_review or cancellation_requested some other way has
		// nobody personally on the hook.
		case EventCancellationRequested, EventReminderStaleReservation,
			EventReservationReassigned, EventPaymentPartialAccepted, EventPaymentPartialConcern,
			EventCancellationAccepted, EventCancellationDeclined:
			if req.ProcessingBy != nil && *req.ProcessingBy != 0 {
				ids = append(ids, *req.ProcessingBy) // the person who reserved it
			} else {
				ids = append(ids, accountIDs...)
			}
		default:
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

func requestHref(req store.Request) string {
	if req.ID == 0 {
		return "/requests"
	}
	return fmt.Sprintf("/requests/%d", req.ID)
}

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
		v.PaidAmount, v.PaidOn = pay.Amount, pay.PaidOn
	case !errors.Is(err, store.ErrNotFound):
		return RequestView{}, err
	}
	return v, nil
}

func resolveRecipients(event string, cfg store.NotificationSetting, app store.MailSettings, v RequestView, accounts []string) (to, cc []string) {
	// One urgent row, two send points: before approval it is the approver's
	// problem; after approval it is Accounts', and only then does management care.
	urgentPreApproval := event == EventRequestUrgent && v.Status == "pending"
	toList := splitList(cfg.ToRecipients)
	if cfg.IncludeRequester {
		toList = append(toList, v.RequesterEmail)
	}
	if cfg.IncludeManager && !(event == EventRequestUrgent && !urgentPreApproval) {
		toList = append(toList, v.ManagerEmail)
	}
	if cfg.IncludeAccounts && !urgentPreApproval {
		toList = append(toList, accounts...)
	}
	ccList := splitList(cfg.CcRecipients)
	if event == EventRequestApproved || (event == EventRequestUrgent && !urgentPreApproval) {
		ccList = append(ccList, splitList(app.ManagementRecipients)...)
	}
	to = dedupe(nil, toList)
	cc = dedupe(to, ccList)
	return to, cc
}

var tokenPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// notifyFields is the single definition of the admin-facing placeholder
// vocabulary (G20). The admin sheet's .hint lists exactly these keys.
func notifyFields(v RequestView) map[string]string {
	return map[string]string{
		"number":          v.Number,
		"amount":          money.FormatPaise(v.Amount),
		"approved_amount": money.FormatPaise(v.ApprovedAmount),
		"payee":           v.Payee,
		"requester":       v.RequesterName,
		"approver":        v.ManagerName,
		"project":         v.Project,
		"head":            v.Head,
		"purpose":         v.Purpose,
		"status":          v.Status,
		"needed_by":       v.NeededBy,
		"submitted_on":    v.SubmittedOn,
		"processing_on":   v.ProcessingOn,
		"paid_amount":     money.FormatPaise(v.PaidAmount),
		"paid_on":         v.PaidOn,
		"link":            v.Link,
	}
}

// NotifyFieldNames is the vocabulary the admin sheet renders in its .hint.
func NotifyFieldNames() []string {
	return []string{"number", "amount", "approved_amount", "payee", "requester", "approver",
		"project", "head", "purpose", "status", "needed_by", "submitted_on", "processing_on", "paid_amount", "paid_on", "link"}
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

// ValidateTemplate reports whether admin-entered text uses only known tokens.
// The rules screen calls it on save so a typo is caught there.
func ValidateTemplate(tmpl string) error {
	_, err := renderTemplate(tmpl, RequestView{})
	return err
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if e := strings.ToLower(strings.TrimSpace(part)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

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

func emails(users []store.User) []string {
	var out []string
	for _, u := range users {
		if u.Email != "" {
			out = append(out, u.Email)
		}
	}
	return out
}

func formatFrom(app store.MailSettings) string {
	name := strings.TrimSpace(app.SMTPFromName)
	addr := strings.TrimSpace(app.SMTPFromAddr)
	if name != "" && addr != "" {
		return fmt.Sprintf("%s <%s>", name, addr)
	}
	return addr
}
