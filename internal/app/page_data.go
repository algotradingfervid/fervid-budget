package app

// PageData, the one value every page template receives, and the two
// small render helpers handlers call to draw a full page or an htmx fragment.

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
	"unicode"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// Warning is one caution above a form, with the heading that says what it is
// about. The body says what to do before saving.
type Warning struct {
	Title string
	Body  string
}

type PageData struct {
	AttachmentMaxMB    int64
	PaymentModeChoices []PaymentModeChoice
	ContextBackHref    string
	ContextBackLabel   string
	Title              string
	User               store.User
	Shell              Shell
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
	AllProjects          []store.Project
	Heads                []store.Head
	Users                []store.User
	Payments             []store.Payment
	PaymentTotal         int64
	Payment              store.Payment
	PaymentAmount        string
	SubmissionKey        string
	RequestPayments      []store.Payment
	RequestFieldErrors   map[string]string
	RequestClearHref     string
	RequestProjectID     int64
	RequestVendorID      int64
	RequestRawAmount     string
	ApprovalError        string
	ApprovalRawAmount    string
	ApprovalNote         string
	RequestRevision      string
	DuplicateReason      string
	Attachments          []store.Attachment
	MonthPlans           []store.MonthPlan
	Audit                []store.AuditEntry
	Reports              []store.ReportRow
	Summary              ReportSummary
	Backups              []string
	BackupRows           []BackupRow
	BackupLocation       string
	BackupDBPath         string
	BackupAttachmentPath string
	RecoveryContact      string
	RecoveryName         string
	LoginNext            string
	ResetEmailAvailable  bool
	ResetToken           string
	VendorTab            string
	VendorModeOptions    []string
	VendorGSTError       string
	From                 string
	To                   string
	Mode                 string
	TargetMonth          string
	SourceMonth          string
	SelectedHeadID       int64
	IncludeVoided        bool
	AuditEntity          string
	AuditAction          string
	AuditActor           string
	AuditFrom            string
	AuditTo              string
	AuditID              string
	AuditPage            store.AuditPage
	CloseGrid            store.GridData
	Locked               bool
	BudgetPlannerJSON    template.JS
	BudgetLineHeads      map[int64]bool
	BudgetInputs         map[int64]string
	BudgetErrors         map[int64]string
	BudgetSaved          int
	BudgetSaveFailed     bool
	ReportProjectID      int64
	ReportHeadID         int64
	ReportBack           string
	ReportURL            string
	ReportContext        string
	ErrorCode            int
	// UrgencyReasonError marks .Error as the missing urgency reason, so the
	// shell ties the alert to that field (data-server-error-for) for the script.
	UrgencyReasonError bool
	RequestID          string

	// Roles admin screen.
	Roles          []store.Role
	Role           store.Role
	PermColumns    []permColumn
	PermMatrix     []permRow
	RoleUserCounts map[int64]int
	NewRole        newRoleForm

	// Users screen.
	AllRoles      []store.Role
	UserRoleIDs   map[int64]map[int64]bool
	Approvers     []store.User
	ApproverNames map[int64]string
	// CanReassignApprover is whether the reader may hand this request's approval
	// on: mayReassignApprover's answer, settled once so the button and the sheet
	// cannot disagree with the route (rbac-8).
	CanReassignApprover bool
	NewUser             newUserForm

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

	// Deactivation is the warning shown before retiring a head or a person that
	// unfinished requests still depend on (F-G-024, F-G-025).
	Deactivation *DeactivationConfirm

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
	// screen is reporting: "taken", "hold" or "not-approved" (F-D-02) — or
	// "unclaimed", when nothing refused anything and the reader simply does
	// not hold a request that is free to take (settlement-2).
	ConflictCause string
	// Outcome is what the payment screen was reached by: "saved" straight after
	// the confirming POST, "duplicate" after a repeat confirm the store refused,
	// "immutable" after a blocked edit, "" on any later visit. It arrives as a
	// one-shot cookie the redirect set and paymentDetail reads once; the
	// "Payment saved … have been notified" banner is tied to it (settlement-5).
	Outcome string
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
	CategoryIDs          map[string]int64
	Ageing               string
	RecoveryCounterparty string
	RecoveryOrder        string
	RecoveryEvents       []store.RecoveryEvent
	RecoveryInput        store.RecoveryInput
	CanRecordRecovery    bool
	RecoveryError        string
	Recoverable          store.RecoverableRow
	RecPayment           store.Payment
	HasPayment           bool

	// Notifications (Phase 5). Notifs is the user's own centre; NotifSettings
	// and MailCfg are the admin rules screen.
	Notifs      []store.Notification
	NotifCounts store.NotificationCounts
	NotifScope  string
	// NotifPage is what the centre is not showing, and NotifOffset is where the
	// window it *is* showing starts. The row query stopped at 100 while the count
	// on the filter strip directly above it had no cap at all, so a user past the
	// cap read "All 137" over 100 rows and nothing said which 37 were missing
	// (F-G-037) — the same defect, and now the same treatment, as the requests
	// list's Page.
	NotifPage     store.NotificationPage
	NotifOffset   int
	NotifSettings []store.NotificationSetting
	MailCfg       store.MailSettings
	NotifFields   []string

	// Warnings are cautions above a form: act before saving. Each carries its
	// own heading, because one form can need two at once (a legacy category and
	// a retired project) and they are not about the same thing. Rendered by the
	// page, not the shell.
	Warnings []Warning
	// LegacyCategory is the deposit category an employee advance was recorded
	// under before deposits and guarantees were their own request type, or ""
	// for every other request. The edit and returned screens use it to name what
	// saving does on their buttons.
	LegacyCategory string
	// NotifDraft is a rule the rules screen refused, with what the admin typed;
	// its sheet comes back open with NotifDraftError inside it (ux-2).
	NotifDraft      *store.NotificationSetting
	NotifDraftError string
}

// Initials renders the signed-in user's avatar text. Two letters at most, so
// the round avatar in the sidebar never overflows.
func (d PageData) Initials() string {
	initials := make([]rune, 0, 2)
	for _, word := range strings.Fields(d.User.Name) {
		for _, letter := range word {
			if unicode.IsLetter(letter) || unicode.IsDigit(letter) {
				initials = append(initials, unicode.ToUpper(letter))
			}
			break
		}
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		return "FB"
	}
	return string(initials)
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
