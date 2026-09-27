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

// PageData is the one value every page template receives. A handler fills
// the fields its screen uses and leaves the rest empty; renderStatus (or
// renderPartial) then adds what every page shares: the signed-in User, Perms,
// the CSRF token, the request id and the navigation Shell.
type PageData struct {
	AttachmentMaxMB    int64
	PaymentModeChoices []PaymentModeChoice
	ContextBackHref    string
	ContextBackLabel   string
	Title              string
	User               store.User
	Shell              Shell
	// Perms is how a template asks whether the signed-in user may do
	// something: a gated control names the resource and action its route
	// needs. It is never nil, so a signed-out page simply hides every control.
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
	// active set a new record may choose from; AllProjects is what the select
	// on an existing record offers, so a record under a retired project keeps
	// its own project selected.
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
	// CanReassignApprover is mayReassignApprover's answer for this request,
	// worked out once so the button and the sheet agree with the route.
	CanReassignApprover bool
	NewUser             newUserForm

	// Vendor master. Vendor.Bank is nil for a caller without vendor_bank:view:
	// the store does not even read the bank columns for them.
	Vendors          []store.Vendor
	Vendor           store.Vendor
	VendorStats      store.VendorStats
	VendorCategories []string
	VendorPaidTotal  int64
	VendorOpenTotal  int
	VendorType       string
	VendorCategory   string
	VendorGap        string
	// VendorEditable is vendor:create on a new vendor and vendor:edit on an
	// existing one, so the template asks one question.
	VendorEditable bool

	// Deactivation is the warning shown before retiring a head or a person that
	// unfinished requests still depend on.
	Deactivation *DeactivationConfirm

	// Payment requests. Request2 is the request a screen is about (named so
	// it is not confused with the *http.Request every handler holds).
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
	// Page describes the part of the requests list on screen (total, offset,
	// whether more rows exist), so the list can say what it is not showing and
	// offer the next page.
	Page store.RequestPage
	// Reminders are the admin-configured reminder thresholds, which the text
	// explaining the wait to a requester quotes.
	Reminders store.ReminderThresholds
	// RecCategory is the recoverable category the form is on. Its Requires
	// flags decide whether the project and counterparty fields are shown.
	RecCategory store.RecoverableCategory
	// Thread is the merged history-and-conversation stream the detail screens
	// render as one `.thread`; RequestAtts is that request's own documents.
	Thread      []store.ThreadEntry
	RequestAtts []store.RequestAttachment
	// Areas are the dashboard's work areas, in the order they are shown.
	Areas []WorkArea

	// Paying requests. Linkable is the queue and picker data; Tab is the
	// queue's selected tab; Settlement is what the confirmation sheet shows
	// (nothing is saved from it); Holder names whoever holds a reservation the
	// reader does not.
	Linkable    store.LinkableSet
	Tab         string
	RecentPaid  []PaidRequestRow
	Settlement  SettlementPreview
	ReserveMine bool
	Holder      string
	// ConflictCause is why the reservation conflict screen is shown: "taken",
	// "hold" or "not-approved" (the store's three refusals), or "unclaimed"
	// when the request is free but the reader has not reserved it.
	ConflictCause string
	// Outcome is how the payment page was reached: "saved" straight after
	// recording it, "duplicate" after a repeated confirm, "immutable" after a
	// blocked edit, "" on any later visit. It comes from a one-time cookie (see
	// redirectToPayment) and decides the banner at the top.
	Outcome string
	// Trail is the partial-review screen's history: the request's and the
	// payment's audit rows merged with the conversation, oldest first.
	Trail []TrailLine
	// Config is the values the Configuration screen edits. (Settings, above,
	// is the same kind of data read by a form that must follow those rules.)
	Config map[string]string

	// Recoverables. RecMetrics and the two rollups drive the dashboard;
	// Recoverables is the register list; Recoverable is the one row the detail
	// screen shows, read by the same query as the list so the two agree.
	RecMetrics       store.RecoverableMetrics
	ByCategory       []store.RecoverableRollup
	ByCounterparty   []store.RecoverableRollup
	Recoverables     []store.RecoverableRow
	RecoverableTotal int64
	Categories       []store.RecoverableCategory
	CategoryUsage    []store.RecoverableCategoryUsage
	CategoryID       int64
	// CategoryIDs maps a rollup's category label to its id, so a dashboard
	// row can link to the list filtered by ?category={id}.
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

	// Notifications. Notifs is the user's own notification centre;
	// NotifSettings and MailCfg are the admin rules screen.
	Notifs      []store.Notification
	NotifCounts store.NotificationCounts
	NotifScope  string
	// NotifPage describes the page of notifications on screen and
	// NotifOffset where it starts, so the centre can offer older rows.
	NotifPage     store.NotificationPage
	NotifOffset   int
	NotifSettings []store.NotificationSetting
	MailCfg       store.MailSettings
	NotifFields   []string

	// Warnings are cautions shown above a form, each with its own heading
	// (one form can need two at once). The page draws them, not the shell.
	Warnings []Warning
	// LegacyCategory is the deposit category an employee advance was recorded
	// under before deposits and guarantees were their own request type, or ""
	// for every other request. The edit and returned screens use it to name what
	// saving does on their buttons.
	LegacyCategory string
	// NotifDraft is a rule the rules screen refused, with what the admin typed;
	// its sheet comes back open with NotifDraftError inside it.
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

// ReportSummary is the totals row of the reports screen: budget, actual and
// variance over the shown rows, and how many rows have each grid status.
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

// render renders a full page with status 200.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	a.renderStatus(w, r, http.StatusOK, name, data)
}

// renderPartial renders one template on its own, without the page shell, for
// htmx requests that replace part of a page. It fills in the user,
// permissions, CSRF token and request id like a full page, so the fragment's
// controls are gated the same way.
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
