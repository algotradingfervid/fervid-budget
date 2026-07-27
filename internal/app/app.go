package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

type App struct {
	cfg  config.Config
	st   *store.Store
	auth *auth.Manager
	tpl  *template.Template
	log  *slog.Logger
	// notify turns workflow events into in-app rows and email; mailer is the
	// transport it uses, kept separately so the rules screen can send a test
	// message without inventing an event.
	notify *notify.Service
	mailer notify.Mailer
}

type PageData struct {
	Title string
	User  store.User
	Shell Shell
	// Perms is how a template asks whether the signed-in user may do something.
	// No template compares a role name: every gated control names the resource
	// and action its route is guarded by, and the permission set answers. It is
	// always non-nil, so a signed-out render simply gates everything off.
	Perms    store.PermissionSet
	CSRF     string
	Error    string
	Notice   string
	Month    string
	Status   string
	Query    string
	Grid     store.GridData
	Projects []store.Project
	// AllProjects is every project, retired ones included. Projects is the
	// active set a new record may choose from; AllProjects is what a row select
	// on an existing record must offer, so a retired project still has an option
	// to be selected and Save cannot silently move the record (F-G-033).
	AllProjects    []store.Project
	Heads          []store.Head
	Users          []store.User
	Payments       []store.Payment
	PaymentTotal   int64
	Payment        store.Payment
	PaymentAmount  string
	Attachments    []store.Attachment
	MonthPlans     []store.MonthPlan
	Audit          []store.AuditEntry
	Reports        []store.ReportRow
	Summary        ReportSummary
	Backups        []string
	From           string
	To             string
	Mode           string
	TargetMonth    string
	SourceMonth    string
	SelectedHeadID int64
	IncludeVoided  bool
	AuditEntity    string
	AuditAction    string
	AuditActor     string
	CloseGrid      store.GridData
	Locked         bool
	BudgetInputs   map[int64]string
	BudgetErrors   map[int64]string
	ErrorCode      int
	RequestID      string

	// Roles admin screen.
	Roles          []store.Role
	Role           store.Role
	PermColumns    []permColumn
	PermMatrix     []permRow
	RoleUserCounts map[int64]int

	// Users screen.
	AllRoles      []store.Role
	UserRoleIDs   map[int64]map[int64]bool
	Approvers     []store.User
	ApproverNames map[int64]string

	// Vendor master. Vendor.Bank is nil for a caller without vendor_bank:view
	// — the store leaves the columns out of the query rather than the template
	// leaving them out of the markup.
	Vendors          []store.Vendor
	Vendor           store.Vendor
	VendorStats      store.VendorStats
	VendorCategories []string
	VendorPaidTotal  int64
	VendorOpenTotal  int
	VendorType       string
	VendorCategory   string
	VendorGap        string
	// VendorEditable is vendor:create on a new record and vendor:edit on an
	// existing one — one question the template would otherwise have to ask two
	// ways. It is derived from the same permission set .Perms is.
	VendorEditable bool

	// Payment requests (Phase 2). Request2 is the request a screen is about;
	// the name keeps it clear of the *http.Request every handler already holds.
	Requests   []store.Request
	Request2   store.Request
	Similar    []store.Request
	Settings   map[string]string
	Scope      string
	Bucket     string
	FormType   string
	TypeFilter string
	Treatment  string
	Counts     map[string]int
	// Page is what the requests list is *not* showing. The 200-row cap used to be
	// silent while the tab count beside it had no cap at all, so a tab promised
	// 214 and the list drew 200 (F-B-16). Total, Offset and Truncated are what
	// let the screen say so and offer the next page.
	Page store.RequestPage
	// Reminders are the admin-configured thresholds, so the copy that explains
	// the wait to a requester reads the live setting instead of a hardcoded
	// "three days" (F-F-03).
	Reminders store.ReminderThresholds
	// RecCategory is the recoverable category the form is currently on, resolved
	// from the active category rows. The conditional project/counterparty fields
	// are revealed from its own Requires flags, so an admin-added category
	// behaves like a seeded one (F-E-02/F-B-17).
	RecCategory store.RecoverableCategory
	// Thread is the merged history-and-conversation stream the detail screens
	// render as one `.thread`; RequestAtts is that request's own documents.
	Thread      []store.ThreadEntry
	RequestAtts []store.RequestAttachment
	// Areas are the dashboard's work areas, in the order they are shown.
	Areas []WorkArea

	// Payment linking, reservation and settlement (Phase 3). Linkable is the
	// queue and picker data; Tab is the queue's `.segmented` selection;
	// Settlement is the view model the confirmation sheet renders and nothing
	// persists; Holder names whoever holds a reservation the reader lost.
	Linkable    store.LinkableSet
	Tab         string
	RecentPaid  []PaidRequestRow
	Settlement  SettlementPreview
	ReserveMine bool
	Holder      string
	// ConflictCause is which of ReserveRequest's three refusals the conflict
	// screen is reporting: "taken", "hold" or "not-approved" (F-D-02).
	ConflictCause string
	// Trail is the one screen whose history spans two entities. Thread is the
	// request's merged stream and cannot carry the payment's audit rows, so the
	// partial review merges the two-entity trail with the conversation itself
	// and renders the result as a single chronological `.thread`.
	Trail []TrailLine
	// Config is the Configuration screen's values. It is deliberately not
	// Settings: Settings is what a *form* consults about the rules it must
	// follow, Config is what the screen that edits those rules renders from.
	Config map[string]string

	// Recoverables (Phase 4). RecMetrics and the two rollups drive the
	// dashboard; Recoverables is the register list; Recoverable is the single
	// row the detail screen ages, read from the same query as the list so the
	// two can never disagree.
	RecMetrics       store.RecoverableMetrics
	ByCategory       []store.RecoverableRollup
	ByCounterparty   []store.RecoverableRollup
	Recoverables     []store.RecoverableRow
	RecoverableTotal int64
	Categories       []store.RecoverableCategory
	CategoryUsage    []store.RecoverableCategoryUsage
	CategoryID       int64
	// CategoryIDs maps a rollup's category label to its id, so the dashboard's
	// by-category row links to ?category={id} — the filter the list actually
	// honours — instead of a free-text search that cannot reproduce the count
	// it was clicked from (F-G-012). RecoverableRollup carries no id of its own.
	CategoryIDs map[string]int64
	Ageing      string
	Recoverable store.RecoverableRow
	RecPayment  store.Payment
	HasPayment  bool

	// Notifications (Phase 5). Notifs is the user's own centre; NotifSettings
	// and MailCfg are the admin rules screen.
	Notifs        []store.Notification
	NotifCounts   store.NotificationCounts
	NotifScope    string
	NotifSettings []store.NotificationSetting
	MailCfg       store.MailSettings
	NotifFields   []string
}

type ReportSummary struct {
	Budget          int64
	Actual          int64
	Variance        int64
	VariancePercent string
	UsedPercent     string
	Rows            int
	Over            int
	Under           int
	OnTrack         int
	NotPaid         int
}

func New(cfg config.Config, st *store.Store) (*http.Server, error) {
	am, err := auth.New(cfg, st)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, st: st, auth: am, log: slog.Default()}
	a.mailer = notify.NewSMTPMailer(st, cfg.SMTPPassword)
	a.notify = notify.NewService(st, a.mailer)
	am.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, status int, message string) {
		a.respondError(w, r, status, message, nil)
	})
	a.tpl = template.Must(template.New("base").Funcs(template.FuncMap{
		"money": money.FormatPaise,
		"short": money.FormatShort,
		"date":  dateText,
		"datep": datepText,
		"select": func(a, b string) template.HTMLAttr {
			if a == b {
				return "selected"
			}
			return ""
		},
		"check": func(v bool) template.HTMLAttr {
			if v {
				return "checked"
			}
			return ""
		},
		"voided":         func(p store.Payment) bool { return p.VoidedAt != nil },
		"usedPct":        usedPct,
		"usedText":       usedText,
		"remainingText":  remainingText,
		"dueClass":       dueClass,
		"dueText":        dueText,
		"roleText":       roleText,
		"boolText":       boolText,
		"actionText":     actionText,
		"actionClass":    actionClass,
		"entityText":     entityText,
		"jsonPretty":     jsonPretty,
		"hasText":        hasText,
		"fileSize":       fileSize,
		"barClass":       barClass,
		"varClass":       varClass,
		"statusText":     statusText,
		"statusFor":      statusForView,
		"planStatusText": planStatusText,
		"paymentMode":    paymentModeText,
		"vendorType":     vendorTypeText,
		"vendorStatus":   vendorStatusText,
		"categories":     categoryChain,
		"plural":         plural,
		// Payment requests. `deref` exists because several request columns are
		// nullable (*int64) and html/template cannot compare a pointer to an int.
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		// requiresKey round-trips a category row through the hidden "requires"
		// field, so the flags→key mapping is not re-derived in HTML.
		"requiresKey": func(c store.RecoverableCategory) string {
			switch {
			case c.RequiresProject && c.RequiresCounterparty:
				return "both"
			case c.RequiresProject:
				return "project"
			case c.RequiresCounterparty:
				return "counterparty"
			default:
				return "none"
			}
		},
		"notifGlyph":   notifGlyph,
		"pillClass":    pillClass,
		"reqStatus":    requestStatusText,
		"typeLabel":    typeLabel,
		"recoverable":  recoverableLabel,
		"inWords":      money.InWords,
		"amountValue":  amountValue,
		"dateLong":     formatLongDate,
		"requestTypes": func() []requestTypeOption { return requestTypeOptions },
		"requestTabs":  func() []requestTab { return requestTabs },
		"approvalTabs": func() []approvalTab { return approvalTabs },
		"threadDot":    threadDot,
		"threadGlyph":  threadGlyph,
		"threadValue":  threadValue,
		"threadField":  threadField,
		"fileKind":     fileKind,
		// Payment linking and settlement (Phase 3). Display only: every rule
		// these read from is enforced in the store.
		"hhmm":          hhmm,
		"since":         since,
		"reservedLabel": reservedLabel,
		"stale":         stale,
		"activeHold":    activeHold,
		"sub":           subPaise,
		"approvedOf":    approvedOf,
		"trailAction":   trailAction,
		"auditTone":     auditTone,
		"auditGlyph":    auditGlyph,
		"auditPhrase":   auditPhrase,
		"trailBody":     trailBody,
		"initials":      initials,
		"paymentModes":  paymentModes,
		"queueTabs":     func() []queueTab { return queueTabs },
		// Configuration. The screen is a rendering of this table, so a later
		// phase adds a section by appending to it and nothing else.
		"configSections": func() []ConfigSection { return configSections },
		// The audit screen's two filters. Built from the strings the store
		// actually writes, so an option list can never drift out of reach of the
		// rows it is meant to select (F-G-004).
		"auditEntities": auditEntities,
		"auditActions":  auditActions,
		"waitingOn":     waitingOn,
		// The requests list's pager arithmetic. `sub` is already taken by the money
		// subtraction the settlement screens use, and an offset can never go
		// negative, so these two are their own pair.
		"add": func(a, b int) int { return a + b },
		"sub0": func(a, b int) int {
			if b <= 0 || a-b < 0 {
				return 0
			}
			return a - b
		},
		"card": func(r store.Request, viewerID int64) requestCardData {
			return requestCardData{Req: r, ViewerID: viewerID}
		},
	}).Parse(templates))

	ctx := contextWithTimeout()
	defer ctx.cancel()
	adminHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return nil, err
	}
	if err := st.EnsureUser(ctx.ctx, cfg.AdminEmail, cfg.AdminName, adminHash, "admin"); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	a.routes(mux)
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.httpObservability(am.Middleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(a.log.Handler(), slog.LevelError),
	}, nil
}

// dateText and datepText are the FuncMap's "date" and "datep". Every timestamp
// the store writes is CURRENT_TIMESTAMP, which SQLite records in UTC, so a
// stamp printed as it comes back is five and a half hours behind the person
// reading it. hhmm has localised since Task 14; these two did not, and the
// reservation screen is the first that prints both for the same event.
func dateText(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func datepText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return dateText(*t)
}

func contextWithTimeout() struct {
	ctx    context.Context
	cancel func()
} {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	return struct {
		ctx    context.Context
		cancel func()
	}{ctx, cancel}
}

func (a *App) routes(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("GET /login", a.loginForm)
	// POST /login is deliberately outside withCSRF: there is no session yet to
	// protect, and a pre-session token buys nothing (F-A-10). It is the single
	// exception to the rule that every mutating POST is wrapped, so it is
	// written down here rather than left for the next reader of withCSRF to
	// rediscover. The body cap is *not* optional, though — withCSRF is where it
	// lives for every other POST, and loginPost is the one unauthenticated
	// endpoint that parses a form, so it caps its own body instead.
	mux.HandleFunc("POST /login", a.withBodyCap(a.loginPost))
	mux.HandleFunc("POST /logout", a.withCSRF(a.logoutPost))
	// "GET /{$}" matches the root and nothing else. Registered as "GET /" it is
	// a catch-all, so every URL the app does not serve — including nav items
	// whose screens are not built yet — silently rendered the dashboard under
	// the wrong address instead of saying the page does not exist.
	// Home is the dashboard: the approved design's screen 01, "one home, four
	// sets of work areas, permission controlled". The nav key for "/" has
	// always been "dashboard" while the variance grid has its own /grid entry;
	// both simply rendered the grid until the dashboard existed.
	mux.Handle("GET /{$}", a.auth.RequireLogin(http.HandlerFunc(a.dashboard)))
	mux.Handle("GET /", a.auth.RequireLogin(http.HandlerFunc(a.notFound)))
	// The variance grid carries the whole budget-versus-actual matrix, so it is
	// gated on the verb the nav item already declares and /export.csv already
	// enforces (F-A-02/F-G-032). It used to ask only for a session, which served
	// the company's plan to a caller holding no role at all.
	mux.Handle("GET /grid", a.auth.RequirePermission("grid", "view", http.HandlerFunc(a.grid)))
	// The work dashboard. It is ungated beyond being signed in, because every
	// area inside it is gated on the queue it opens: a person with no areas is
	// told nothing is waiting on them rather than refused the page.
	mux.Handle("GET /dashboard", a.auth.RequireLogin(http.HandlerFunc(a.dashboard)))
	mux.Handle("GET /months", a.auth.RequirePermission("month", "view", http.HandlerFunc(a.months)))
	mux.Handle("POST /months", a.auth.RequirePermission("month", "create", http.HandlerFunc(a.withCSRF(a.monthCreate))))
	mux.Handle("GET /payments/new", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentForm)))
	mux.Handle("GET /payments/new/options", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentPickerOptions)))
	mux.Handle("GET /payments", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.payments)))
	// The write that records a payment and closes a request is gated at least as
	// tightly as its own read-only preview (F-D-10): the preview asks for
	// payment:settle, so the writer asks for payment:create *and* payment:settle.
	// payment:mark_partial cannot be a route gate, because it applies only when
	// settlement=partial — paymentCreate checks it itself.
	mux.Handle("POST /payments", a.auth.RequirePermission("payment", "create",
		a.auth.RequirePermission("payment", "settle", http.HandlerFunc(a.withCSRF(a.paymentCreate)))))
	mux.Handle("GET /payments/{id}", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.paymentDetail)))
	mux.Handle("GET /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.paymentEditForm)))
	mux.Handle("POST /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.withCSRF(a.paymentEdit))))
	mux.Handle("POST /payments/{id}/void", a.auth.RequirePermission("payment", "void", http.HandlerFunc(a.withCSRF(a.paymentVoid))))
	mux.Handle("POST /payments/{id}/attachments", a.auth.RequirePermission("attachment", "create", http.HandlerFunc(a.withCSRF(a.attachmentUpload))))
	// Two tables, two routes. /attachments/{id} reads payment_attachments;
	// request documents live in request_attachments with an unrelated id
	// sequence, so serving them from the same URL handed one reader another
	// reader's bank advice (F-A-05). Both routes re-check the request the
	// attachment hangs off, because attachment:view is a Requester grant and
	// the verb alone is not permission to read a particular file (F-A-01).
	mux.Handle("GET /attachments/{id}", a.auth.RequirePermission("attachment", "view", http.HandlerFunc(a.attachmentDownload)))
	mux.Handle("GET /requests/{id}/attachments/{attachmentID}", a.auth.RequirePermission("attachment", "view", http.HandlerFunc(a.requestAttachmentDownload)))
	mux.Handle("GET /export.csv", a.auth.RequirePermission("grid", "export", http.HandlerFunc(a.exportGrid)))
	mux.Handle("GET /reports/monthly", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/projects", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/heads", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/ytd.csv", a.auth.RequirePermission("report", "export", http.HandlerFunc(a.exportYTD)))

	// Recoverables (Phase 4). /recoverables/list.csv is a literal final segment
	// and Go 1.22+ ServeMux gives it precedence over the {id} wildcard, so the
	// export never reaches the detail handler.
	mux.Handle("GET /recoverables", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverablesDashboard)))
	mux.Handle("GET /recoverables/list", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverablesList)))
	mux.Handle("GET /recoverables/list.csv", a.auth.RequirePermission("recoverable_report", "export", http.HandlerFunc(a.exportRecoverable)))
	mux.Handle("GET /recoverables/{id}", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverableDetail)))

	// The user's own notification centre (Phase 5). Authenticated session only:
	// every row is already scoped to the caller by the store, so a permission
	// verb would add nothing.
	mux.Handle("GET /notifications", a.auth.RequireLogin(http.HandlerFunc(a.notificationCentre)))
	mux.Handle("POST /notifications/read", a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(a.notificationsMarkAllRead))))
	mux.Handle("GET /notifications/{id}/open", a.auth.RequireLogin(http.HandlerFunc(a.notificationOpen)))

	// The admin rule editor (D7). A different screen from the centre above, with
	// a different audience, so it is the one behind a permission verb.
	mux.Handle("GET /admin/notifications", a.auth.RequirePermission("notification", "view", http.HandlerFunc(a.adminNotifications)))
	mux.Handle("POST /admin/notifications/smtp", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.adminNotificationsSMTP))))
	mux.Handle("POST /admin/notifications/events/{event}", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.adminNotificationEventSave))))
	mux.Handle("POST /admin/notifications/test", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.adminNotificationsTest))))
	mux.Handle("GET /budgets", a.auth.RequirePermission("budget", "view", http.HandlerFunc(a.budgets)))
	mux.Handle("POST /budgets", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.withCSRF(a.budgetSave))))
	mux.Handle("POST /months/{month}/lock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.lockMonth))))
	mux.Handle("POST /months/{month}/unlock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.unlockMonth))))
	mux.Handle("GET /projects", a.auth.RequirePermission("project", "view", http.HandlerFunc(a.projects)))
	mux.Handle("POST /projects", a.auth.RequirePermission("project", "edit", http.HandlerFunc(a.withCSRF(a.projectSave))))
	mux.Handle("GET /heads", a.auth.RequirePermission("head", "view", http.HandlerFunc(a.heads)))
	mux.Handle("POST /heads", a.auth.RequirePermission("head", "edit", http.HandlerFunc(a.withCSRF(a.headSave))))
	// Literal segments beat the {id} wildcard in ServeMux's specificity rules,
	// so /vendors/new is the form and never a vendor whose id parses to zero.
	mux.Handle("GET /vendors", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorsList)))
	mux.Handle("GET /vendors/new", a.auth.RequirePermission("vendor", "create", http.HandlerFunc(a.vendorNew)))
	mux.Handle("GET /vendors/search", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorSearch)))
	mux.Handle("GET /vendors/{id}", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorDetail)))
	mux.Handle("POST /vendors", a.auth.RequirePermission("vendor", "create", http.HandlerFunc(a.withCSRF(a.vendorCreate))))
	mux.Handle("POST /vendors/{id}", a.auth.RequirePermission("vendor", "edit", http.HandlerFunc(a.withCSRF(a.vendorUpdate))))
	// Payment requests (Phase 2). Literal segments beat the {id} wildcard, so
	// /requests/new is always the form and never a request whose id parses to
	// zero. There is no draft route and no second submit step: D1 makes create
	// and submit one POST.
	mux.Handle("GET /requests", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requests)))
	mux.Handle("GET /requests/export.csv", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestsExport)))
	mux.Handle("GET /requests/new", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.requestNew)))
	mux.Handle("GET /requests/new/fields", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.requestFormFields)))
	mux.Handle("POST /requests/duplicate-check", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.withCSRF(a.requestDuplicateCheck))))
	mux.Handle("POST /requests", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.withCSRF(a.requestCreate))))
	mux.Handle("GET /requests/{id}/submitted", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestSubmitted)))
	// Payment linking and settlement (Phase 3). Reservation has its own verbs
	// (spec D2) precisely so "may take work" and "may take work off somebody
	// else" stop being the same grant.
	mux.Handle("POST /requests/{id}/record-payment", a.auth.RequirePermission("reservation", "reserve", http.HandlerFunc(a.withCSRF(a.requestRecordPayment))))
	// The settlement preview is a POST because it carries the form, not because
	// it changes anything: it is pure and writes nothing (D8).
	mux.Handle("POST /requests/{id}/settlement-preview", a.auth.RequirePermission("payment", "settle", http.HandlerFunc(a.withCSRF(a.settlementPreview))))
	// Giving a reservation up and taking one off somebody else are one screen
	// and two verbs, and either one is a reason to open it: the holder arrives
	// to release, an administrator arrives to reassign. release and reassign are
	// independent cells of the permission matrix, so gating the route on one of
	// them answered 403 to the reader the accounts queue and the conflict screen
	// both link here. The route therefore asks only for a session, and
	// reservationForm asks for the verb that matches the caller's standing —
	// each POST still carries its own gate, so reading the page never confers
	// the power to move somebody else's work.
	mux.Handle("GET /requests/{id}/reservation", a.auth.RequireLogin(http.HandlerFunc(a.reservationForm)))
	mux.Handle("POST /requests/{id}/release", a.auth.RequirePermission("reservation", "release", http.HandlerFunc(a.withCSRF(a.requestRelease))))
	mux.Handle("POST /requests/{id}/reassign", a.auth.RequirePermission("reservation", "reassign", http.HandlerFunc(a.withCSRF(a.requestReassign))))
	// The 26-hour nudge (Q6). Reading it needs payment:process — the grant that
	// puts a person in front of reserved rows at all — and every choice it
	// offers is gated again on the verb that choice actually needs.
	mux.Handle("GET /requests/{id}/reservation/stale", a.auth.RequirePermission("payment", "process", http.HandlerFunc(a.requestStale)))
	// L7: a hold is Accounts' pause button, not the approver's. payment:hold is
	// one grant for both directions, because the person who can stop a payment
	// is the person who must be able to start it again.
	mux.Handle("POST /requests/{id}/hold", a.auth.RequirePermission("payment", "hold", http.HandlerFunc(a.withCSRF(a.requestHold))))
	mux.Handle("POST /requests/{id}/unhold", a.auth.RequirePermission("payment", "hold", http.HandlerFunc(a.withCSRF(a.requestUnhold))))
	mux.Handle("GET /accounts-queue", a.auth.RequirePermission("payment", "process", http.HandlerFunc(a.accountsQueue)))
	// A partial settlement is the one outcome Accounts cannot close on its own,
	// so the manager gets a screen of their own (S11). Reading it needs only
	// request:view — everyone on the thread may follow what is happening — while
	// both decisions are gated on approval:accept_partial, because accepting a
	// shortfall and disputing it are two answers to the same question.
	mux.Handle("GET /requests/{id}/partial-review", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestPartialReview)))
	mux.Handle("POST /requests/{id}/accept-partial", a.auth.RequirePermission("approval", "accept_partial", http.HandlerFunc(a.withCSRF(a.requestAcceptPartial))))
	mux.Handle("POST /requests/{id}/raise-concern", a.auth.RequirePermission("approval", "accept_partial", http.HandlerFunc(a.withCSRF(a.requestRaiseConcern))))
	// One detail screen for every audience: the action bar changes on
	// permission, the page does not. Each decision posts to its own route so
	// the gate is the verb the decision needs, not the one that opened the page.
	mux.Handle("GET /requests/{id}", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestDetail)))
	mux.Handle("GET /requests/{id}/edit", a.auth.RequirePermission("request", "edit", http.HandlerFunc(a.requestEditForm)))
	mux.Handle("POST /requests/{id}/edit", a.auth.RequirePermission("request", "edit", http.HandlerFunc(a.withCSRF(a.requestEdit))))
	mux.Handle("POST /requests/{id}/comment", a.auth.RequirePermission("request", "comment", http.HandlerFunc(a.withCSRF(a.requestComment))))
	mux.Handle("POST /requests/{id}/withdraw", a.auth.RequirePermission("request", "withdraw", http.HandlerFunc(a.withCSRF(a.requestWithdraw))))
	mux.Handle("POST /requests/{id}/reraise", a.auth.RequirePermission("request", "reraise", http.HandlerFunc(a.withCSRF(a.requestReraise))))
	mux.Handle("POST /requests/{id}/approve", a.auth.RequirePermission("approval", "approve", http.HandlerFunc(a.withCSRF(a.requestApprove))))
	mux.Handle("POST /requests/{id}/return", a.auth.RequirePermission("approval", "return", http.HandlerFunc(a.withCSRF(a.requestReturn))))
	mux.Handle("POST /requests/{id}/reject", a.auth.RequirePermission("approval", "reject", http.HandlerFunc(a.withCSRF(a.requestReject))))
	// A7: handing an approval on to a different approver, with a reason and a
	// history entry. The path is deliberately not /reassign — that one is the
	// payment reservation's, on reservation:reassign — because moving an
	// approval and moving a reservation are different acts by different people
	// (F-A-06/F-C-02). store.ReassignRequest carried every rule and had no door.
	mux.Handle("POST /requests/{id}/reassign-approver", a.auth.RequirePermission("approval", "reassign", http.HandlerFunc(a.withCSRF(a.requestReassignApprover))))
	// The cancellation flow. GET /cancel is the employee asking and POST /cancel
	// is the approver cancelling outright: two verbs on one path, because they
	// are the same sentence said by two people, and ServeMux gates each method
	// on the permission its own actor needs.
	mux.Handle("GET /requests/{id}/cancel", a.auth.RequirePermission("request", "cancel", http.HandlerFunc(a.requestCancelForm)))
	mux.Handle("POST /requests/{id}/cancel-request", a.auth.RequirePermission("request", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancelAsk))))
	mux.Handle("POST /requests/{id}/cancel", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancelOutright))))
	mux.Handle("GET /requests/{id}/cancellation", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.requestCancellationForm)))
	mux.Handle("POST /requests/{id}/cancellation", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancellationDecide))))
	// The manager queue is its own screen (A15), gated on the verb that lets a
	// person decide rather than on the one that lets them read.
	mux.Handle("GET /approvals", a.auth.RequirePermission("approval", "approve", http.HandlerFunc(a.approvals)))
	mux.Handle("GET /users", a.auth.RequirePermission("user", "view", http.HandlerFunc(a.users)))
	// One handler, two verbs: the screen renders ＋ Add user behind user:create
	// and the Edit sheet behind user:edit, and the route has to agree with both
	// or a legitimately configured role gets a control whose submit 403s
	// (F-A-07). Which verb applies depends on the posted id, which cannot be read
	// before the form is parsed — so the route refuses anybody holding neither,
	// keeping the "permission gate outside withCSRF" ordering every other POST
	// has, and userSave then demands the one the pressed control was gated on.
	mux.Handle("POST /users", a.requireAnyOf("user", []string{"create", "edit"}, http.HandlerFunc(a.withCSRF(a.userSave))))
	mux.Handle("GET /roles", a.auth.RequirePermission("role", "view", http.HandlerFunc(a.rolesPage)))
	mux.Handle("POST /roles", a.auth.RequirePermission("role", "edit", http.HandlerFunc(a.withCSRF(a.rolesSave))))
	mux.Handle("POST /roles/new", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCreate))))
	mux.Handle("POST /roles/{id}/copy", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCopy))))
	mux.Handle("POST /roles/{id}/delete", a.auth.RequirePermission("role", "delete", http.HandlerFunc(a.withCSRF(a.roleDelete))))
	mux.Handle("GET /configuration", a.auth.RequirePermission("config", "view", http.HandlerFunc(a.configuration)))
	mux.Handle("POST /configuration", a.auth.RequirePermission("config", "edit", http.HandlerFunc(a.withCSRF(a.configurationSave))))
	// The screen is gated on config{view}; the category mutation keeps its own
	// Phase-1 verb, which is what the store audits against.
	mux.Handle("POST /configuration/recoverable-categories", a.auth.RequirePermission("recoverable_category", "edit", http.HandlerFunc(a.withCSRF(a.recoverableCategorySave))))
	mux.Handle("GET /audit", a.auth.RequirePermission("audit", "view", http.HandlerFunc(a.auditLog)))
	mux.Handle("GET /backups", a.auth.RequirePermission("backup", "view", http.HandlerFunc(a.backups)))
	mux.Handle("POST /backups", a.auth.RequirePermission("backup", "create", http.HandlerFunc(a.withCSRF(a.backupCreate))))
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	a.renderStatus(w, r, http.StatusOK, name, data)
}

// renderPartial executes one template without the shell. Every htmx fragment
// that is a piece of a page — rather than a page in its own right — goes
// through it: renderStatus already skips shell CONSTRUCTION when HX-Request is
// present, and this is the rendering half of the same contract, for templates
// that never call "top"/"bottom" at all. Permissions still apply, because a
// fragment gates its controls exactly as the full page would.
func (a *App) renderPartial(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	data.User = auth.CurrentUser(r)
	data.Perms = a.auth.Permissions(data.User)
	data.CSRF = a.auth.EnsureCSRF(w, r)
	data.RequestID = requestID(r)
	data.Shell = Shell{Chrome: chromeNone}

	var buf bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&buf, name, data); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "That section could not be rendered.", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(buf.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "fragment write failed", "request_id", requestID(r), "error", err)
	}
}

// requireAnyOf is RequirePermission for a route one of whose actions depends on
// the submitted form. It refuses a caller holding none of the named actions with
// the same status and the same sentence auth.RequirePermission uses, so the
// refusal is indistinguishable, and it runs where every other permission gate
// runs: outside withCSRF, before anything is parsed. The handler behind it must
// still demand the specific action the request turns out to need — this only
// establishes that the caller has business here at all.
func (a *App) requireAnyOf(resource string, actions []string, next http.Handler) http.Handler {
	return a.auth.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.CurrentUser(r)
		for _, action := range actions {
			if a.auth.Can(u, resource, action) {
				next.ServeHTTP(w, r)
				return
			}
		}
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to perform this action.", nil)
	}))
}

// withBodyCap is the body limit half of withCSRF, on its own, for the one POST
// that is deliberately outside the token check. An unauthenticated endpoint
// that calls ParseForm on an unbounded body is a free memory sink; every other
// POST refuses at maxRequestBodyBytes and so does this one (F-A-10).
func (a *App) withBodyCap(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		fn(w, r)
	}
}

func (a *App) withCSRF(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		var err error
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			err = r.ParseMultipartForm(1 << 20)
		} else {
			err = r.ParseForm()
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				a.respondError(w, r, http.StatusRequestEntityTooLarge, "The submitted form is too large.", err)
			} else {
				a.respondError(w, r, http.StatusBadRequest, "The submitted form could not be read.", err)
			}
			return
		}
		if !a.auth.CheckCSRF(r) {
			a.respondError(w, r, http.StatusForbidden, "Your form session expired. Refresh the page and try again.", nil)
			return
		}
		fn(w, r)
	}
}

func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "login", PageData{Title: "Login"})
}

func (a *App) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderStatus(w, r, http.StatusBadRequest, "login", PageData{Title: "Login", Error: "Invalid form"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	u, err := a.st.UserByEmail(r.Context(), email)
	if err == nil {
		if locked, until, lockErr := a.st.LoginLocked(r.Context(), email); lockErr == nil && locked {
			a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "login_failed", EntityType: "user", EntityID: &u.ID, Summary: "Login blocked until " + until.Format(time.RFC3339), IP: r.RemoteAddr})
			a.render(w, r, "login", PageData{Title: "Login", Error: "Too many failed attempts. Try again later."})
			return
		}
	}
	if err != nil || !u.Active || !auth.CheckPassword(u.PasswordHash, r.FormValue("password")) {
		if _, failureErr := a.st.RecordFailedLogin(r.Context(), email); failureErr != nil && !errors.Is(failureErr, store.ErrNotFound) {
			a.log.ErrorContext(r.Context(), "failed to record login attempt", "request_id", requestID(r), "error", failureErr)
		}
		var actorID *int64
		actorName := email
		if err == nil {
			actorID = &u.ID
			actorName = u.Name
		}
		a.recordAudit(r, store.AuditInput{ActorID: actorID, ActorName: actorName, Action: "login_failed", EntityType: "user", EntityID: actorID, Summary: "Failed login attempt", IP: r.RemoteAddr})
		a.render(w, r, "login", PageData{Title: "Login", Error: "Invalid email or password"})
		return
	}
	if err := a.st.ResetLoginFailures(r.Context(), email); err != nil {
		a.log.ErrorContext(r.Context(), "failed to reset login attempts", "request_id", requestID(r), "error", err)
	}
	a.auth.Login(w, r, u)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "login", EntityType: "user", EntityID: &u.ID, Summary: "Logged in", IP: r.RemoteAddr})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) logoutPost(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	if u.ID != 0 {
		a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "logout", EntityType: "user", EntityID: &u.ID, Summary: "Logged out", IP: r.RemoteAddr})
	}
	a.auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) grid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	grid, err := a.st.Grid(r.Context(), month, status, q)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	closeGrid, err := a.st.Grid(r.Context(), month, "", "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// grid:view opens the budget matrix. The Recent Payments panel is a second
	// question: it carries amounts, payees and live /payments/{id} links, so it
	// is gated on payment:view separately (F-A-02/F-G-032) and the caller who
	// lacks that verb is simply handed no rows. The gate is here rather than in
	// the template because a screen must not be trusted to hide data the handler
	// loaded — /payments answers 403 to exactly this caller.
	u := auth.CurrentUser(r)
	var payments []store.Payment
	if a.auth.Can(u, "payment", "view") {
		payments, err = a.st.ListPayments(r.Context(), store.PaymentListOptions{
			Month: month, Status: "active", Limit: 10,
			Scope: a.auth.Scope(u, "payment"), ViewerID: u.ID,
		})
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	a.render(w, r, "grid", PageData{Title: "Variance Grid", Grid: grid, CloseGrid: closeGrid, Month: month, Status: status, Query: q, Payments: payments})
}

// paymentForm has two branches and no third. Without ?request= it is the
// picker: every payment belongs to exactly one approved request, so choosing
// one is the first step rather than an optional extra. With ?request= it is the
// entry screen for a reservation the caller holds.
func (a *App) paymentForm(w http.ResponseWriter, r *http.Request) {
	linkedID := parseID(r.URL.Query().Get("request"))
	if linkedID == 0 {
		data, err := a.pickerData(r)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		a.render(w, r, "payment_pick_request", data)
		return
	}
	a.paymentEntry(w, r, linkedID)
}

func (a *App) payments(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := queryDefault(r, "status", "active")
	q := r.URL.Query().Get("q")
	// The `payment` data scope is one of the two scoped resources, and until the
	// viewer reached ListPayments a role built with Payments · Own received the
	// whole ledger (F-A-04/F-G-003). The store mirrors requestWhere: only "own"
	// and "assigned" narrow.
	u := auth.CurrentUser(r)
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{
		Month: month, Status: status, Query: q,
		Scope: a.auth.Scope(u, "payment"), ViewerID: u.ID,
	})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var total int64
	for _, payment := range payments {
		if payment.VoidedAt == nil {
			total += payment.Amount
		}
	}
	a.render(w, r, "payments", PageData{Title: "Payments", Month: month, Status: status, Query: q, Payments: payments, PaymentTotal: total, Locked: a.st.IsLocked(r.Context(), month)})
}

// paymentCreate is the only user-reachable way a payment is created, and it
// creates nothing that is not linked to a request the caller holds (X5). The
// free-standing create survives in the store as a seed/import helper only.
func (a *App) paymentCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	linkedID := parseID(r.FormValue("request_id"))
	if linkedID == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Payments must be linked to an approved request.", nil)
		return
	}
	settlement := r.FormValue("settlement")
	// Writing off a shortfall to the approver's queue is its own decision, and
	// payment:mark_partial is the verb the vocabulary declares for it. It cannot
	// be a route gate because it applies to one value of one field (F-D-10).
	if settlement == "partial" && !a.auth.Can(u, "payment", "mark_partial") {
		a.respondError(w, r, http.StatusForbidden,
			"You do not have permission to record a partial payment.", nil)
		return
	}
	in, err := paymentInput(r)
	partialReason := r.FormValue("partial_reason")
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	var payID int64
	if err == nil {
		payID, err = a.st.RecordPaymentForRequest(r.Context(), u, linkedID, in, settlement, partialReason, attachment)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		// A double-confirm (back button, double tap) must not look like a
		// failure: the payment this request needed already exists, so go to it.
		if pay, perr := a.st.PaymentForRequest(r.Context(), linkedID); perr == nil {
			http.Redirect(w, r, fmt.Sprintf("/payments/%d", pay.ID), http.StatusSeeOther)
			return
		}
		a.settlementError(w, r, linkedID, in, r.FormValue("amount"), settlement, partialReason, err)
		return
	}
	// A settled payment closes the request; a partial one asks the approver to
	// accept the shortfall. They are different events because they ask
	// different people for different things.
	if settlement == "settled" {
		a.fire(r, notify.EventPaymentSettled, linkedID)
	} else {
		a.fire(r, notify.EventPaymentPartialReview, linkedID)
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", payID), http.StatusSeeOther)
}

// paymentDetail is one screen with two readings. A payment linked to a request
// is the end of that request's story, so it shows what was approved beside what
// was paid and the whole trail from submission to settlement — and offers
// nothing that would change it (S12). A historical, request-less payment is
// still a ledger row and keeps the pre-Phase-3 screen until Phase 6 redraws the
// ledger (X6).
func (a *App) paymentDetail(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	p, err := a.st.Payment(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	atts, err := a.st.Attachments(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if p.RequestID != nil {
		req, rerr := a.st.Request(r.Context(), *p.RequestID)
		if rerr != nil {
			a.respondStoreError(w, r, rerr)
			return
		}
		// Half this screen is the request: its number, its people, its whole
		// history. Reading it therefore obeys the same row scope /requests/{id}
		// obeys, or payment:view becomes a way around Q5/R6.
		u := auth.CurrentUser(r)
		if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
			a.respondError(w, r, http.StatusForbidden, "You cannot see the request behind this payment.", nil)
			return
		}
		// The trail the screen shows spans both entities: the request from
		// submission to approval, then the payment from reservation to
		// settlement. There is no store read across entity types, so the two
		// audit listings are merged and ordered oldest-first.
		trail, terr := a.mergedTrail(r, req.ID, p.ID)
		if terr != nil {
			a.respondStoreError(w, r, terr)
			return
		}
		// The proof list is both halves too: the bank advice Accounts uploaded
		// and the invoice the request came in with (payment-detail.html).
		reqAtts, aerr := a.st.RequestAttachments(r.Context(), req.ID)
		if aerr != nil {
			a.respondStoreError(w, r, aerr)
			return
		}
		a.render(w, r, "payment_detail", PageData{
			Title: "Payment · " + req.Number, Payment: p, Request2: req,
			Attachments: atts, RequestAtts: reqAtts, Audit: trail,
		})
		return
	}
	audit, err := a.st.Audit(r.Context(), "payment", id, 50)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "payment_detail", PageData{Title: "Payment Detail", Payment: p, Attachments: atts, Audit: audit, Locked: a.st.IsLocked(r.Context(), p.PaidOn[:7])})
}

func (a *App) paymentEditForm(w http.ResponseWriter, r *http.Request) {
	p, err := a.st.Payment(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// S12: a payment that settled a request cannot be edited, and the store
	// refuses the write. Serving the form anyway would let somebody fill in a
	// screen whose Save can only ever fail, so the URL goes where the payment
	// actually lives.
	if p.RequestID != nil {
		http.Redirect(w, r, fmt.Sprintf("/payments/%d", p.ID), http.StatusSeeOther)
		return
	}
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Month is the payment's own, so "Back to grid" returns to the month this
	// payment belongs to rather than to / — which was the grid before Phase 4 and
	// is the dashboard now (F-G-029).
	a.render(w, r, "payment_edit_form", PageData{Title: "Edit Payment", Payment: p, Heads: heads,
		Month: p.PaidOn[:7], Locked: a.st.IsLocked(r.Context(), p.PaidOn[:7]) || p.VoidedAt != nil})
}

func (a *App) paymentEdit(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	in, err := paymentInput(r)
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	if err == nil {
		err = a.st.UpdatePaymentWithAttachment(r.Context(), auth.CurrentUser(r), id, in, attachment)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		heads, headsErr := a.st.ListHeads(r.Context(), true)
		if headsErr != nil {
			a.respondStoreError(w, r, headsErr)
			return
		}
		p := paymentFromInput(in)
		p.ID = id
		locked := validDateInput(in.PaidOn) && a.st.IsLocked(r.Context(), in.PaidOn[:7])
		a.renderStatus(w, r, status, "payment_edit_form", PageData{Title: "Edit Payment", Payment: p, PaymentAmount: r.FormValue("amount"), Heads: heads, Locked: locked, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

func (a *App) paymentVoid(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	err := a.st.VoidPayment(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if next := r.FormValue("next"); strings.HasPrefix(next, "/payments") {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// canReadPayment is the ownership rule behind every attachment route, and it is
// the same rule paymentDetail applies before rendering a linked payment: half of
// what an attachment discloses belongs to the request, so reading it obeys the
// request's row scope "or payment:view becomes a way around Q5/R6"
// (paymentDetail's own comment, and this is the route it was written about —
// F-A-01/F-A-03).
//
// A request-less historical payment has no row scope to consult, so it falls
// back to the ledger's own verb plus the payment data scope.
func (a *App) canReadPayment(r *http.Request, p store.Payment) bool {
	u := auth.CurrentUser(r)
	if p.RequestID != nil {
		req, err := a.st.Request(r.Context(), *p.RequestID)
		if err != nil {
			return false
		}
		return canViewRequest(a.auth.Scope(u, "request"), u, req)
	}
	return a.auth.Can(u, "payment", "view") &&
		paymentScopeReaches(a.auth.Scope(u, "payment"), u.ID, p)
}

// paymentScopeReaches mirrors ListPayments' scope filter exactly, so a caller
// who cannot see a payment in the ledger cannot see it by id either: "own" and
// "assigned" narrow to what the caller entered, and everything else — including
// the empty scope — leaves the ledger unrestricted.
func paymentScopeReaches(scope string, viewerID int64, p store.Payment) bool {
	switch scope {
	case "own", "assigned":
		return p.EnteredBy == viewerID
	default:
		return true
	}
}

// notFoundAttachment is the single refusal for every attachment the caller may
// not have. It is a 404 and not a 403 on purpose: attachment ids are small
// sequential integers, and a distinguishable refusal turns the route into an
// enumeration oracle for the whole table.
func (a *App) notFoundAttachment(w http.ResponseWriter, r *http.Request, reason string, id int64) {
	a.log.WarnContext(r.Context(), "attachment refused",
		"request_id", requestID(r), "attachment_id", id, "reason", reason)
	a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
}

func (a *App) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	// The write path needs the same ownership check the read path needs, in the
	// other direction: the attachments on a payment *are* the evidence that it
	// happened as recorded, and evidence any signed-in user can add to is not
	// evidence (F-A-03). AddAttachment separately refuses a linked payment, which
	// makes this unreachable today; the check stays so the hole does not reopen
	// the day a free-standing payment becomes creatable again.
	payment, err := a.st.Payment(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "payment not found", id)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !a.canReadPayment(r, payment) {
		a.notFoundAttachment(w, r, "payment out of the caller's scope", id)
		return
	}
	attachment, attachmentPath, err := a.stageUploadedAttachment(r)
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		a.respondStoreError(w, r, err)
		return
	}
	if attachment == nil {
		a.respondError(w, r, http.StatusBadRequest, "Choose a file to upload.", nil)
		return
	}
	if err := a.st.AddAttachment(r.Context(), auth.CurrentUser(r), id, attachment.OriginalName, attachment.StoredPath, attachment.MimeType, attachment.SizeBytes); err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// attachmentDownload serves a *payment* document — a bank advice, a proof of
// payment — and only to somebody the payment's own request is visible to.
func (a *App) attachmentDownload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	att, payment, err := a.st.AttachmentWithPayment(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such payment attachment", id)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !a.canReadPayment(r, payment) {
		a.notFoundAttachment(w, r, "payment out of the caller's scope", id)
		return
	}
	a.serveAttachmentFile(w, r, att.ID, att.StoredPath, att.OriginalName, att.MimeType)
}

// requestAttachmentDownload serves a *request* document — the invoice or
// quotation the request came in with. It reads request_attachments, checks the
// document really belongs to the {id} in the path, and scopes it by that
// request. Both halves matter: without the table split the two id sequences
// collide (F-A-05), and without the scope check attachment:view would read
// every request's documents (F-A-01).
func (a *App) requestAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	attachmentID := parseID(r.PathValue("attachmentID"))
	att, err := a.st.RequestAttachmentByID(r.Context(), attachmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such request attachment", attachmentID)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	// The document has to belong to the request in the URL. Otherwise the path's
	// {id} is decoration and the route is /attachments/{id} again under a longer
	// name.
	if att.RequestID != pathID(r) {
		a.notFoundAttachment(w, r, "attachment belongs to another request", attachmentID)
		return
	}
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), att.RequestID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such request", attachmentID)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		a.notFoundAttachment(w, r, "request out of the caller's scope", attachmentID)
		return
	}
	a.serveAttachmentFile(w, r, att.ID, att.StoredPath, att.OriginalName, att.MimeType)
}

// serveAttachmentFile is the path-safety and streaming tail both download
// routes share, so the two can never drift about what is inside AttachmentDir.
func (a *App) serveAttachmentFile(w http.ResponseWriter, r *http.Request, id int64, storedPath, originalName, mimeType string) {
	root, err := filepath.Abs(a.cfg.AttachmentDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The attachment could not be opened.", err)
		return
	}
	stored, err := filepath.Abs(storedPath)
	if err != nil || !strings.HasPrefix(stored, root+string(os.PathSeparator)) {
		a.log.WarnContext(r.Context(), "unsafe attachment path rejected", "request_id", requestID(r), "attachment_id", id)
		a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
		return
	}
	if _, err := os.Stat(stored); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
		} else {
			a.respondError(w, r, http.StatusInternalServerError, "The attachment could not be opened.", err)
		}
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": originalName}))
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	http.ServeFile(w, r, stored)
}

func (a *App) stageUploadedAttachment(r *http.Request) (*store.AttachmentInput, string, error) {
	file, header, err := r.FormFile("attachment")
	if err != nil {
		// No file, or no multipart body to hold one: either way the caller sent
		// no attachment, which is not an error on any of the forms that offer one.
		if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read attachment: %w", err)
	}
	defer file.Close()
	if strings.TrimSpace(header.Filename) == "" {
		return nil, "", nil
	}
	name := fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(header.Filename))
	stored := filepath.Join(a.cfg.AttachmentDir, name)
	out, err := os.OpenFile(stored, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, "", fmt.Errorf("create attachment: %w", err)
	}
	cleanup := func() {
		if removeErr := os.Remove(stored); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			a.log.ErrorContext(r.Context(), "attachment cleanup failed", "request_id", requestID(r), "path", stored, "error", removeErr)
		}
	}
	size, copyErr := io.Copy(out, io.LimitReader(file, maxAttachmentBytes+1))
	cerr := out.Close()
	if copyErr != nil {
		cleanup()
		return nil, "", fmt.Errorf("store attachment: %w", copyErr)
	}
	if cerr != nil {
		cleanup()
		return nil, "", fmt.Errorf("close attachment: %w", cerr)
	}
	if size > maxAttachmentBytes {
		cleanup()
		return nil, "", fmt.Errorf("%w: files must be 20 MiB or smaller", store.ErrValidation)
	}
	return &store.AttachmentInput{
		OriginalName: filepath.Base(header.Filename),
		StoredPath:   stored,
		MimeType:     header.Header.Get("Content-Type"),
		SizeBytes:    size,
	}, stored, nil
}

func (a *App) months(w http.ResponseWriter, r *http.Request) {
	plans, err := a.st.ListMonthPlans(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	target := defaultTargetMonth(plans)
	source := previousMonth(target)
	a.render(w, r, "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source})
}

func (a *App) monthCreate(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.FormValue("target_month"))
	source := ""
	if r.FormValue("source_mode") == "copy" {
		source = strings.TrimSpace(r.FormValue("source_month"))
	}
	if err := a.st.CreateMonthPlan(r.Context(), auth.CurrentUser(r), target, source); err != nil {
		plans, listErr := a.st.ListMonthPlans(r.Context())
		if listErr != nil {
			a.respondStoreError(w, r, listErr)
			return
		}
		// The status is the store's, not a hardcoded 400: a locked month is a 409
		// everywhere else and must be a 409 here too (F-G-026).
		a.renderStatus(w, r, reRenderStatus(err), "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, "/budgets?month="+target, http.StatusSeeOther)
}

func (a *App) budgets(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	grid, err := a.st.Grid(r.Context(), month, "", "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads})
}

func (a *App) budgetSave(w http.ResponseWriter, r *http.Request) {
	month := strings.TrimSpace(r.FormValue("month"))
	if !validMonthInput(month) {
		a.respondError(w, r, http.StatusBadRequest, "A valid month is required.", nil)
		return
	}
	u := auth.CurrentUser(r)
	inputs := make(map[int64]string)
	fieldErrors := make(map[int64]string)
	var updates []store.BudgetInput
	// One unreadable field is one unreadable field. The whole batch used to be
	// abandoned for any single failure, and because the screen renders ₹0.00 for
	// every unbudgeted head — a value ParsePaise refuses — a month that had never
	// been budgeted could not be budgeted at all (F-G-028). Every field that
	// parses is now applied; the ones that do not are named back to the operator.
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "budget_") {
			continue
		}
		headID, idErr := strconv.ParseInt(strings.TrimPrefix(k, "budget_"), 10, 64)
		raw := ""
		if len(v) > 0 {
			raw = v[0]
		}
		if idErr != nil || headID <= 0 {
			continue
		}
		inputs[headID] = raw
		amt, amountErr := parseBudgetPaise(raw)
		if amountErr != nil {
			fieldErrors[headID] = amountErr.Error()
			continue
		}
		updates = append(updates, store.BudgetInput{HeadID: headID, Amount: amt})
	}
	var saveErr error
	if len(updates) > 0 {
		saveErr = a.st.SetBudgets(r.Context(), u, month, updates)
	}
	if saveErr != nil || len(fieldErrors) > 0 {
		grid, gridErr := a.st.Grid(r.Context(), month, "", "")
		heads, headsErr := a.st.ListHeads(r.Context(), true)
		if gridErr != nil || headsErr != nil {
			a.respondStoreError(w, r, errors.Join(gridErr, headsErr))
			return
		}
		message := friendly(saveErr)
		if saveErr == nil {
			// The wording keeps "invalid budget amount" from the message this
			// replaces, because that phrase is what the reader — and the shipped
			// regression guard — recognises. What it no longer says is that
			// everything was refused, because it no longer is.
			message = fmt.Sprintf("%d invalid budget %s left unchanged; everything else was saved.",
				len(fieldErrors), plural(len(fieldErrors), "amount was", "amounts were"))
		}
		// reRenderStatus keeps a locked month a 409 here too (F-G-026).
		a.renderStatus(w, r, reRenderStatus(saveErr), "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads, BudgetInputs: inputs, BudgetErrors: fieldErrors, Error: message})
		return
	}
	http.Redirect(w, r, "/budgets?month="+month, http.StatusSeeOther)
}

// parseBudgetPaise reads a budget figure. A budget of zero is a real and
// meaningful number — "this head has no budget this month" is the state the
// screen renders by default, as ₹0.00 — so an empty or zero field is accepted
// and money.ParsePaise, which rightly refuses a non-positive *payment*, is not
// the parser for it. A negative budget is still refused; SetBudgets refuses it
// again.
func parseBudgetPaise(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSpace(strings.TrimPrefix(s, "₹"))
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("enter a number, or leave it empty for no budget")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("enter a number, or leave it empty for no budget")
	}
	rounded := math.Round(f * 100)
	if rounded >= float64(math.MaxInt64) || rounded <= -float64(math.MaxInt64) {
		return 0, fmt.Errorf("that budget is too large")
	}
	paise := int64(rounded)
	if paise < 0 {
		return 0, fmt.Errorf("a budget cannot be negative")
	}
	return paise, nil
}

// reRenderStatus is the status a handler that re-renders its own screen answers
// with. Routing through storeErrorStatus is what keeps one condition to one
// code: a locked month was a 409 through respondStoreError and a 400 on the two
// screens that draw their own error (F-G-026).
func reRenderStatus(err error) int {
	if err == nil {
		return http.StatusBadRequest
	}
	return storeErrorStatus(err)
}

// lockMonth and unlockMonth land on /grid?month=, which is the one screen that
// renders the lock and its reason. They used to redirect to /?month=, which was
// the variance grid before Phase 4 made / the dashboard — so an operator locked
// a month and was dropped on a page that read the month parameter not at all and
// confirmed nothing (F-G-019, and the same root cause as F-G-029).
func (a *App) lockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.LockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/grid?month="+month, http.StatusSeeOther)
}

func (a *App) unlockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.UnlockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/grid?month="+month, http.StatusSeeOther)
}

func (a *App) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.st.ListProjects(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "projects", PageData{Title: "Projects", Projects: projects})
}

func (a *App) projectSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	pid, err := a.st.UpsertProject(r.Context(), id, r.FormValue("name"), r.FormValue("active") == "on", sortOrder)
	if err == nil {
		u := auth.CurrentUser(r)
		action := "update"
		if id == 0 {
			action = "create"
		}
		a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: action, EntityType: "project", EntityID: &pid, Summary: strings.Title(action) + "d project " + strings.TrimSpace(r.FormValue("name"))})
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (a *App) heads(w http.ResponseWriter, r *http.Request) {
	data, err := a.headsPageData(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "heads", data)
}

// headsPageData is the heads screen's whole state, split out so the contract it
// owes the markup is testable without a rendered page.
//
// Two project lists, because the screen asks two different questions. Projects
// is what a *new* head may be filed under: active only, per T12. AllProjects is
// what an *existing* row's select must offer, and it has to be every project —
// a head whose project was retired had no matching option, so the browser
// preselected the first one and that row's own Save moved the head there with no
// edit of any kind (F-G-033). Server-side there is no way to tell "the operator
// chose this project" from "the browser defaulted to it", which is why the fix is
// the option list and not a validation rule.
func (a *App) headsPageData(ctx context.Context) (PageData, error) {
	heads, err := a.st.ListHeads(ctx, false)
	if err != nil {
		return PageData{}, err
	}
	projects, err := a.st.ListProjects(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	allProjects, err := a.st.ListProjects(ctx, false)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Heads", Heads: heads, Projects: projects, AllProjects: allProjects}, nil
}

func (a *App) headSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	projectID := parseID(r.FormValue("project_id"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	hid, err := a.st.UpsertHead(r.Context(), id, projectID, r.FormValue("name"), r.FormValue("due_day"), r.FormValue("active") == "on", sortOrder)
	if err == nil {
		u := auth.CurrentUser(r)
		action := "update"
		if id == 0 {
			action = "create"
		}
		a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: action, EntityType: "head", EntityID: &hid, Summary: strings.Title(action) + "d head " + strings.TrimSpace(r.FormValue("name"))})
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/heads", http.StatusSeeOther)
}

func (a *App) users(w http.ResponseWriter, r *http.Request) {
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	assigned := map[int64]map[int64]bool{}
	names := map[int64]string{}
	var approvers []store.User
	for _, u := range users {
		names[u.ID] = u.Name
		if u.Active {
			approvers = append(approvers, u)
		}
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		set := map[int64]bool{}
		for _, ur := range urs {
			set[ur.ID] = true
		}
		assigned[u.ID] = set
	}
	a.render(w, r, "users", PageData{
		Title:         "Users",
		Users:         users,
		AllRoles:      roles,
		UserRoleIDs:   assigned,
		Approvers:     approvers,
		ApproverNames: names,
	})
}

func (a *App) userSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	// F-A-07: creating and editing are two grants and this one handler does both.
	// The verb it demands is therefore the verb the control the caller pressed is
	// gated on, not whichever one happened to be on the route.
	verb := "edit"
	if id == 0 {
		verb = "create"
	}
	if !a.auth.Can(auth.CurrentUser(r), "user", verb) {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to perform this action.", nil)
		return
	}
	hash := ""
	if pw := r.FormValue("password"); pw != "" {
		if err := validatePassword(pw); err != nil {
			a.respondError(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		var err error
		hash, err = auth.HashPassword(pw)
		if err != nil {
			a.respondError(w, r, http.StatusInternalServerError, "The password could not be secured.", err)
			return
		}
	}
	active := r.FormValue("active") == "on"
	var err error
	var savedID int64
	if id == 0 {
		if hash == "" {
			a.respondError(w, r, http.StatusBadRequest, "A password is required.", nil)
			return
		}
		savedID, err = a.st.CreateUser(r.Context(), r.FormValue("email"), r.FormValue("name"), hash, r.FormValue("role"), active)
	} else {
		current := auth.CurrentUser(r)
		if id == current.ID && (!active || r.FormValue("role") != "admin") {
			a.respondError(w, r, http.StatusBadRequest, "You cannot deactivate or demote your own administrator account.", nil)
			return
		}
		// One form, one submit, one transaction. The profile, the role assignment
		// and the default approver used to be three independent store calls, so a
		// save refused by the second left the first one's rename committed and
		// unaudited (F-G-034). Role assignment and the default approver apply only
		// to an existing user; the create path relies on CreateUser's own
		// default-role assignment.
		var roleIDs []int64
		for _, raw := range r.Form["role_ids"] {
			if v := parseID(raw); v != 0 {
				roleIDs = append(roleIDs, v)
			}
		}
		err = a.st.SaveUser(r.Context(), current, store.UserSaveInput{
			ID:                id,
			Name:              r.FormValue("name"),
			Role:              r.FormValue("role"),
			Active:            active,
			PasswordHash:      hash,
			RoleIDs:           roleIDs,
			DefaultApproverID: parseID(r.FormValue("default_approver_id")),
		})
		savedID = id
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	action := "update"
	if id == 0 {
		action = "create"
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: action, EntityType: "user", EntityID: &savedID, Summary: strings.Title(action) + "d user " + strings.ToLower(strings.TrimSpace(r.FormValue("email")))})
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// requestReassignApprover hands a pending request to a different approver, with
// a reason, and records it as its own `approval_reassign` event (coverage A7).
//
// store.ReassignRequest has always carried every rule this needs — pending only,
// a real approver at the other end, G8, a mandatory reason, its own audit action
// — and had no HTTP caller at all, so `approval:reassign` was a grant an
// administrator could give and revoke with nothing behind it (F-A-06/F-C-02).
// This is also the repair for a request routed to somebody who cannot approve
// (F-A-08) and for a stranded approval (F-G-025).
//
// Who may do it: the route asks for `approval:reassign`, and the caller's request
// data scope must reach the row. That admits the request's own approver handing
// it on, and an administrator rescuing one whose approver cannot act — which is
// the case that needs rescuing, so requiring `manager_id == actor` here would
// close the only door out of it.
func (a *App) requestReassignApprover(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		// 404 and not 403: a request outside the caller's scope answers the same
		// way whether or not it exists (F-G-002).
		a.respondError(w, r, http.StatusNotFound, "The requested record was not found.", nil)
		return
	}
	to := parseID(r.FormValue("manager_id"))
	if to == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Choose the approver this request should go to.", nil)
		return
	}
	if strings.TrimSpace(r.FormValue("reason")) == "" {
		a.respondError(w, r, http.StatusBadRequest, "Give a reason for the reassignment.", nil)
		return
	}
	if err := a.st.ReassignRequest(r.Context(), u, req.ID, to, r.FormValue("reason")); err != nil {
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		a.respondError(w, r, status, friendly(err), err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

func (a *App) rolesPage(w http.ResponseWriter, r *http.Request) {
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	selected := parseID(r.URL.Query().Get("role"))
	if selected == 0 && len(roles) > 0 {
		selected = roles[0].ID
	}
	role, err := a.st.Role(r.Context(), selected)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.respondStoreError(w, r, err)
		return
	}
	grants, scopes, err := a.st.RolePermissions(r.Context(), selected)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	counts := map[int64]int{}
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	for _, u := range users {
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		for _, ur := range urs {
			counts[ur.ID]++
		}
	}
	a.render(w, r, "roles", PageData{
		Title:          "Roles",
		Roles:          roles,
		Role:           role,
		PermColumns:    permColumns,
		PermMatrix:     buildPermMatrix(grants, scopes),
		RoleUserCounts: counts,
	})
}

// rolesSave persists the matrix. Grants are the union of the cells that were
// ticked — each expanding to every canonical action behind it — and the
// individual Advanced checkboxes. The union is order-independent, and it is
// what lets a cell be only partially granted. UpdateRolePermissions then
// replaces the role's whole grant set, so anything absent here is revoked.
func (a *App) rolesSave(w http.ResponseWriter, r *http.Request) {
	roleID := parseID(r.FormValue("role_id"))
	grants := expandCells(r.Form["cell"])
	seen := map[store.Grant]bool{}
	for _, g := range grants {
		seen[g] = true
	}
	for _, raw := range r.Form["perm"] {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		g := store.Grant{Resource: parts[0], Action: parts[1]}
		if seen[g] {
			continue
		}
		seen[g] = true
		grants = append(grants, g)
	}
	// The two matrices carry two scope radio groups. They must not share a name:
	// same-name radios are one group across the whole form, so enabling the
	// mobile set would silently clear the desktop selection. The desktop group
	// wins when it is enabled; the mobile group is the fallback. An empty value
	// is the explicit "None" state and stores no scope row at all.
	var scopes []store.ScopeGrant
	for _, res := range scopedMatrixResources() {
		v := strings.TrimSpace(r.FormValue("scope_" + res))
		if v == "" {
			v = strings.TrimSpace(r.FormValue("m_scope_" + res))
		}
		if v != "" {
			scopes = append(scopes, store.ScopeGrant{Resource: res, Scope: v})
		}
	}
	// Screen the grants before writing anything. UpdateRole and
	// UpdateRolePermissions are two transactions, so a grant rejected by the
	// second would otherwise leave the first one's rename committed. Every grant
	// is re-validated inside UpdateRolePermissions as well — this is a guard
	// against a half-applied save, not the security boundary.
	for _, g := range grants {
		if !store.ValidGrant(g.Resource, g.Action) {
			a.respondError(w, r, http.StatusBadRequest, "That permission does not exist.", nil)
			return
		}
	}
	if err := a.st.UpdateRole(r.Context(), auth.CurrentUser(r), roleID, r.FormValue("name"), r.FormValue("description")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if err := a.st.UpdateRolePermissions(r.Context(), auth.CurrentUser(r), roleID, grants, scopes); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(roleID, 10), http.StatusSeeOther)
}

func (a *App) roleCreate(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CreateRole(r.Context(), auth.CurrentUser(r), r.FormValue("name"), r.FormValue("description"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) roleCopy(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CopyRole(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("name"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) roleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteRole(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		// respondStoreError replaces every ErrForbidden with "You do not have
		// permission to perform this action.", which told the holder of all 66
		// grants that they held none (F-G-023). Both refusals this route can
		// produce — a system role, and a role somebody still holds — are rules
		// with reasons, so the reason is what the reader gets.
		if errors.Is(err, store.ErrForbidden) {
			a.respondError(w, r, storeErrorStatus(err), friendly(err), err)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}

func (a *App) auditLog(w http.ResponseWriter, r *http.Request) {
	entity := strings.TrimSpace(r.URL.Query().Get("entity"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	actor := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("actor")))
	audit, err := a.st.Audit(r.Context(), entity, parseID(r.URL.Query().Get("id")), 1000)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if action != "" || actor != "" {
		filtered := audit[:0]
		for _, entry := range audit {
			if action != "" && entry.Action != action {
				continue
			}
			if actor != "" && !strings.Contains(strings.ToLower(entry.ActorName), actor) {
				continue
			}
			filtered = append(filtered, entry)
		}
		audit = filtered
	}
	audit = a.auditWithinRequestScope(r, audit)
	if len(audit) > 200 {
		audit = audit[:200]
	}
	a.render(w, r, "audit", PageData{Title: "Audit Log", Audit: audit, AuditEntity: entity, AuditAction: action, AuditActor: r.URL.Query().Get("actor")})
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
	if scope == store.ScopeAll {
		return entries
	}
	visible := map[int64]bool{}
	out := entries[:0]
	for _, entry := range entries {
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
			visible[id] = err == nil && canViewRequest(scope, u, req)
		}
		if visible[id] {
			out = append(out, entry)
		}
	}
	return out
}

func (a *App) backups(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(a.cfg.BackupDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "Backups could not be listed.", err)
		return
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "backup-") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	a.render(w, r, "backups", PageData{Title: "Backups", Backups: names})
}

func (a *App) backupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := store.Backup(r.Context(), a.cfg.DBPath, a.cfg.AttachmentDir, a.cfg.BackupDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The backup could not be created.", err)
		return
	}
	if err := store.PruneBackups(a.cfg.BackupDir, a.cfg.BackupKeepDays, time.Now()); err != nil {
		a.log.WarnContext(r.Context(), "backup pruning failed", "request_id", requestID(r), "error", err)
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "create", EntityType: "backup", Summary: "Created backup " + info.Path})
	http.Redirect(w, r, "/backups", http.StatusSeeOther)
}

func (a *App) report(w http.ResponseWriter, r *http.Request) {
	from := validMonthOrCurrent(queryDefault(r, "from", r.URL.Query().Get("month")))
	to := validMonthOrFallback(r.URL.Query().Get("to"), from)
	if from > to {
		from, to = to, from
	}
	mode := strings.TrimPrefix(r.URL.Path, "/reports/")
	rows, err := a.st.Report(r.Context(), from, to, mode)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if mode != "monthly" {
		rows = nonEmptyReportRows(rows)
	}
	a.render(w, r, "reports", PageData{Title: "Reports", Reports: rows, Summary: summarizeReports(rows), From: from, To: to, Mode: mode})
}

func (a *App) exportGrid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	grid, err := a.st.Grid(r.Context(), month, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported grid " + month})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Project", "Head", "Budget", "Actual", "Variance", "Variance %", "Status"})
	for _, row := range grid.Rows {
		_ = cw.Write([]string{row.Project, row.Head, csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent, row.Status})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="variance-`+month+`.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

// reportLevels are the three groupings store.Report knows, which are also the
// three /reports/* paths. The CSV takes the level as a parameter so a download
// pressed from the Projects or Monthly tab exports that tab (F-G-031); it used to
// hardcode "heads" whichever tab it was pressed from.
var reportLevels = map[string]bool{"monthly": true, "projects": true, "heads": true}

func (a *App) exportYTD(w http.ResponseWriter, r *http.Request) {
	year := queryDefault(r, "year", time.Now().Format("2006"))
	from := validMonthOrFallback(r.URL.Query().Get("from"), year+"-01")
	to := validMonthOrFallback(r.URL.Query().Get("to"), year+"-12")
	if from > to {
		from, to = to, from
	}
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	if !reportLevels[level] {
		level = "heads"
	}
	rows, err := a.st.Report(r.Context(), from, to, level)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported " + level + " report " + from + " to " + to})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Period", "Project", "Head", "Budget", "Actual", "Variance", "Variance %"})
	for _, row := range rows {
		_ = cw.Write([]string{row.Period, row.Project, row.Head, csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="report-`+from+`-to-`+to+`.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

func paymentInput(r *http.Request) (store.PaymentInput, error) {
	in := store.PaymentInput{
		HeadID:      parseID(r.FormValue("head_id")),
		PaidOn:      r.FormValue("paid_on"),
		VendorPayee: r.FormValue("vendor_payee"),
		PaymentMode: r.FormValue("payment_mode"),
		InvoiceNo:   r.FormValue("invoice_no"),
		ReferenceNo: r.FormValue("reference_no"),
		Remarks:     r.FormValue("remarks"),
	}
	amount, err := money.ParsePaise(r.FormValue("amount"))
	in.Amount = amount
	if err != nil {
		return in, fmt.Errorf("%w: invalid amount; enter a valid payment amount", store.ErrValidation)
	}
	return in, err
}

func queryDefault(r *http.Request, key, fallback string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	return fallback
}

func pathID(r *http.Request) int64 { return parseID(r.PathValue("id")) }

func parseID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func validMonthInput(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func validMonthOrCurrent(s string) string {
	return validMonthOrFallback(s, time.Now().Format("2006-01"))
}

func validMonthOrFallback(s, fallback string) string {
	if validMonthInput(s) {
		return s
	}
	if !validMonthInput(fallback) {
		return time.Now().Format("2006-01")
	}
	return fallback
}

func validDateInput(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// validatePassword owns the whole password rule, so every rejection is a 400
// naming what is missing. It used to check the length alone and leave the
// letter-and-digit rule to auth.HashPassword, which reports a refusal as a
// failure to hash — a 500 reading "The password could not be secured." for a
// perfectly ordinary validation event (F-A-11/F-G-036). The 12-character
// minimum is this layer's own, stricter than auth's bootstrap-compatible 8.
func validatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	return auth.ValidatePassword(password)
}

func nonEmptyReportRows(rows []store.ReportRow) []store.ReportRow {
	out := rows[:0]
	for _, row := range rows {
		if row.Budget != 0 || row.Actual != 0 {
			out = append(out, row)
		}
	}
	return out
}

func defaultTargetMonth(plans []store.MonthPlan) string {
	now := time.Now().Format("2006-01")
	if len(plans) == 0 || plans[0].Month < now {
		return now
	}
	return shiftMonth(plans[0].Month, 1)
}

func previousMonth(month string) string {
	return shiftMonth(month, -1)
}

func shiftMonth(month string, delta int) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Now().AddDate(0, delta, 0).Format("2006-01")
	}
	return t.AddDate(0, delta, 0).Format("2006-01")
}

func paymentFromInput(in store.PaymentInput) store.Payment {
	return store.Payment{
		HeadID:      in.HeadID,
		PaidOn:      in.PaidOn,
		Amount:      in.Amount,
		VendorPayee: in.VendorPayee,
		PaymentMode: in.PaymentMode,
		InvoiceNo:   in.InvoiceNo,
		ReferenceNo: in.ReferenceNo,
		Remarks:     in.Remarks,
	}
}

func summarizeReports(rows []store.ReportRow) ReportSummary {
	var s ReportSummary
	s.Rows = len(rows)
	for _, row := range rows {
		s.Budget += row.Budget
		s.Actual += row.Actual
		switch statusForView(row.Budget, row.Actual) {
		case "over":
			s.Over++
		case "under":
			s.Under++
		case "on-track":
			s.OnTrack++
		case "not-paid":
			s.NotPaid++
		}
	}
	s.Variance = s.Budget - s.Actual
	s.VariancePercent = money.Percent(s.Variance, s.Budget)
	s.UsedPercent = usedText(s.Budget, s.Actual)
	return s
}

// csvAmount is how money leaves the application in a file. Every export used to
// write money.FormatPaise, so an Actual cell read "₹1,50,000.00" — quoted,
// because the Indian grouping commas would otherwise break the field, and
// Number() of it is NaN (F-G-030). An export is the hop where the data stops
// being a rendered report and starts being data, so it carries rupees to two
// decimals with no symbol, no grouping and no quoting, and the reader formats.
func csvAmount(paise int64) string {
	sign := ""
	if paise < 0 {
		sign, paise = "-", -paise
	}
	return fmt.Sprintf("%s%d.%02d", sign, paise/100, paise%100)
}

// friendly turns a store error into the sentence a person reads. The
// `validation failed: ` sentinel is stripped: it is how the code recognises the
// class of error, never something anybody wants to find at the top of a form
// (F-B-08).
func friendly(err error) string {
	switch {
	case errors.Is(err, store.ErrLockedMonth):
		return "This month is locked. Unlock it with a reason before changing budgets or payments."
	case errors.Is(err, store.ErrDuplicate):
		return "A record with this name already exists."
	case errors.Is(err, store.ErrInactiveHead):
		return "This project/head is inactive."
	case errors.Is(err, store.ErrValidation):
		// The sentence only, never the sentinel. The case of the first letter is
		// left exactly as the store wrote it: these strings are sentence
		// fragments the screens embed as well as show ("Correct it and confirm
		// again"), and a great many of them are asserted verbatim.
		return strings.TrimPrefix(err.Error(), store.ErrValidation.Error()+": ")
	case errors.Is(err, store.ErrForbidden):
		// A refusal that carries a reason keeps it. The state conflicts on the
		// settlement path — "reserve this request before recording its payment",
		// "this request is on hold" — are rules, not faults, and fell through to
		// the generic sentence that also covers a genuine server error (F-A-09).
		// The sentinel prefix is trimmed because "forbidden: …" is not a sentence
		// a person reads; ErrForbidden alone, with nothing wrapped, still gets the
		// permission wording.
		if msg := strings.TrimPrefix(err.Error(), store.ErrForbidden.Error()+": "); msg != err.Error() {
			return upperFirst(msg)
		}
		return "You do not have permission to perform this action."
	default:
		return "Something went wrong while processing your request."
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func usedPct(budget, actual int64) string {
	if budget <= 0 || actual <= 0 {
		return "0%"
	}
	pct := (float64(actual) / float64(budget)) * 100
	if pct > 118 {
		pct = 118
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func usedText(budget, actual int64) string {
	if budget <= 0 || actual <= 0 {
		return "0%"
	}
	pct := (float64(actual) / float64(budget)) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

func remainingText(budget, actual int64) string {
	if budget <= 0 {
		return "N/A"
	}
	remaining := budget - actual
	pct := (float64(remaining) / float64(budget)) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

func barClass(status string) string {
	switch status {
	case "over":
		return "is-over"
	case "under":
		return "is-under"
	case "on-track":
		return "is-ontrack"
	case "unbudgeted":
		return "is-unbudgeted"
	default:
		return ""
	}
}

func varClass(variance int64) string {
	switch {
	case variance > 0:
		return "varpos"
	case variance < 0:
		return "varneg"
	default:
		return "varzero"
	}
}

func statusText(status string) string {
	switch status {
	case "over":
		return "Over budget"
	case "under":
		return "Under budget"
	case "on-track":
		return "On track"
	case "not-paid":
		return "Not paid"
	case "unbudgeted":
		return "Unbudgeted spend"
	default:
		return status
	}
}

func planStatusText(status string) string {
	switch status {
	case "locked":
		return "Locked"
	case "open":
		return "Open"
	default:
		return status
	}
}

func statusForView(budget, actual int64) string {
	switch {
	case actual == 0:
		return "not-paid"
	case budget == 0 && actual > 0:
		return "unbudgeted"
	case budget > 0 && actual > budget:
		return "over"
	case budget > 0 && actual == budget:
		return "on-track"
	default:
		return "under"
	}
}

func dueClass(dueDay, month, status string) string {
	if status != "not-paid" {
		return "settled"
	}
	day, ok := dueDayNumber(dueDay)
	if !ok || !validMonthInput(month) {
		return "none"
	}
	now := time.Now()
	if now.Format("2006-01") != month {
		target, err := time.Parse("2006-01-02", fmt.Sprintf("%s-%02d", month, day))
		if err != nil {
			return "none"
		}
		if target.Before(now) {
			return "overdue"
		}
		return "upcoming"
	}
	switch {
	case now.Day() > day:
		return "overdue"
	case now.Day() == day:
		return "today"
	default:
		return "upcoming"
	}
}

func dueText(dueDay, month, status string) string {
	class := dueClass(dueDay, month, status)
	switch class {
	case "settled":
		return "Settled"
	case "overdue":
		return "Overdue"
	case "today":
		return "Due today"
	case "upcoming":
		return "Upcoming"
	default:
		if strings.TrimSpace(dueDay) == "" {
			return "No due day"
		}
		return "Due " + dueDay
	}
}

func dueDayNumber(dueDay string) (int, bool) {
	digits := ""
	for _, r := range dueDay {
		if r >= '0' && r <= '9' {
			digits += string(r)
		} else if digits != "" {
			break
		}
	}
	if digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil && n >= 1 && n <= 31
}

func roleText(role string) string {
	switch role {
	case "data_entry":
		return "Data entry"
	case "admin":
		return "Admin"
	default:
		return role
	}
}

func paymentModeText(mode string) string {
	switch mode {
	case "bank_transfer":
		return "Bank transfer"
	case "cash":
		return "Cash"
	case "cheque":
		return "Cheque"
	case "card":
		return "Card"
	case "upi":
		return "UPI"
	case "other":
		return "Other"
	default:
		return mode
	}
}

func vendorTypeText(t string) string {
	switch t {
	case "company":
		return "Company"
	case "proprietor":
		return "Proprietor"
	case "individual":
		return "Individual"
	default:
		return t
	}
}

func vendorStatusText(status string) string {
	switch status {
	case "active":
		return "Active"
	case "inactive":
		return "Inactive"
	default:
		return status
	}
}

// categoryChain renders the stored comma-separated categories the way the
// approved list screen does, as a "·" chain. store.SplitCategories is what
// decides the boundaries, so the filter's option list and this reading can
// never disagree about what a category is.
func categoryChain(raw string) string {
	return strings.Join(store.SplitCategories(raw), " · ")
}

// plural picks between two whole phrases rather than appending an "s", because
// "1 vendor has" and "2 vendors have" differ in more than one place.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func boolText(v bool) string {
	if v {
		return "Active"
	}
	return "Inactive"
}

// actionText spells an audit action for a person. It covers every action string
// the store writes, because an action with no case renders its raw lower-case
// identifier in the audit pill — which is what `settings` did on every
// configuration save (F-G-004). auditActions below is built from the same list,
// so the filter can never offer an action this cannot name.
func actionText(action string) string {
	switch action {
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
		"void", "lock", "unlock", "settings", "export",
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

func hasText(s string) bool {
	return strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "null"
}

func jsonPretty(s string) string {
	if !hasText(s) {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}

func fileSize(size int64) string {
	switch {
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
