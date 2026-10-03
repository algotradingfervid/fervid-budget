// models.go holds the plain data types the store reads and writes and the
// sentinel errors callers branch on. The types carry no behaviour. Money is
// always int64 paise (1/100 rupee); months are "YYYY-MM" and dates
// "YYYY-MM-DD" strings.

package store

import (
	"errors"
	"time"
)

// Sentinel errors. Store methods wrap them with a readable message
// (fmt.Errorf("%w: ...")), so callers test them with errors.Is and the
// handlers map them to HTTP answers.
var (
	// ErrNotFound means the requested row does not exist (a 404).
	ErrNotFound = errors.New("not found")
	// ErrForbidden means the actor may not do this to this row (a 403), or the
	// row is not in a state that allows it.
	ErrForbidden = errors.New("forbidden")
	// ErrLockedMonth means the change falls in a month an administrator locked.
	ErrLockedMonth = errors.New("month is locked")
	// ErrValidation means the input is wrong; the message says what to fix.
	ErrValidation = errors.New("validation failed")
	// ErrDuplicate means a unique value (email, name, number) is already taken.
	ErrDuplicate = errors.New("duplicate value")
	// ErrInactiveHead means the payment names a head or project that is inactive.
	ErrInactiveHead = errors.New("project or head is inactive")
)

// User is one account. Role is the older single-role column, kept in step with
// the assigned roles ("admin" or "data_entry"); permissions come from the roles.
type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	Role         string
	Active       bool
	// DefaultApproverID is who approves this user's requests unless the request
	// names someone else; 0 means none. A user is never their own default
	// approver — SaveUser refuses it.
	DefaultApproverID int64
	// SessionVersion goes up when the password changes or the account is
	// deactivated, which ends every existing session.
	SessionVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// requestCache is an opaque value the auth layer attaches to the signed-in
	// user for the length of one request (its permission cache). The store
	// never reads it; users loaded from the database never carry one.
	requestCache any
}

// WithRequestCache returns a copy of u carrying c. See User.requestCache.
func (u User) WithRequestCache(c any) User {
	u.requestCache = c
	return u
}

// RequestCache returns the value WithRequestCache attached, or nil.
func (u User) RequestCache() any { return u.requestCache }

// Project groups budget heads.
type Project struct {
	ID        int64
	Name      string
	Active    bool
	SortOrder int
	CreatedAt time.Time
}

// Head is a budget line inside a project that budgets and payments are
// recorded against. DueDay is an optional day of the month ("1"–"31").
type Head struct {
	ID        int64
	ProjectID int64
	Project   string
	Name      string
	DueDay    string
	Active    bool
	SortOrder int
	CreatedAt time.Time
}

// RecoverableCategory is one admin-configurable kind of recoverable payment.
//
// Code is the stable identity and never changes once created: requests store it
// in payment_requests.recoverable_category and the request validator resolves
// the field rules by it, so renaming a category must not orphan its requests.
// Name is the display label an admin is free to edit.
type RecoverableCategory struct {
	ID                   int64
	Code                 string
	Name                 string
	RequiresProject      bool
	RequiresCounterparty bool
	Active               bool
	SortOrder            int
	CreatedAt            time.Time
}

// RecoverableCategoryUsage is a category plus the number of payment requests
// that reference it. The Configuration screen's "In use" column shows it so an
// admin can see what deactivating a category would strand.
type RecoverableCategoryUsage struct {
	RecoverableCategory
	InUse int
}

// Budget is the amount planned for one head in one month.
type Budget struct {
	ID        int64
	HeadID    int64
	Month     string
	Amount    int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MonthPlan is one row of the monthly plans screen: a month's status, who
// created it, and its budget-versus-actual totals.
type MonthPlan struct {
	Month         string
	Status        string
	SourceMonth   string
	CreatedByName string
	CreatedAt     *time.Time
	Budget        int64
	Actual        int64
	Variance      int64
	UsedPercent   string
	HeadCount     int
	Locked        bool
	LockedAt      *time.Time
	LockReason    string
}

// Payment is one ledger row: money that left the bank against a head (or, for
// a recoverable request, against no head).
type Payment struct {
	ID            int64
	HeadID        int64
	ProjectID     int64
	Project       string
	Head          string
	PaidOn        string
	Amount        int64
	VendorPayee   string
	PaymentMode   string
	InvoiceNo     string
	ReferenceNo   string
	Remarks       string
	EnteredBy     int64
	EnteredByName string
	UpdatedBy     *int64
	VoidedBy      *int64
	VoidReason    string
	VoidedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// RequestID is nil for a payment entered directly in the ledger, which
	// keeps it editable and voidable. A payment that settles a request has it
	// set and cannot be edited, voided or given new attachments.
	RequestID     *int64
	Settlement    string // "" (historical) | "settled" | "partial"
	PartialReason string
	// Treatment is the linked request's treatment ("" for a payment with no
	// request). A "recoverable" payment has no head, so a screen reads this to
	// name it rather than print an empty "Project / Head".
	Treatment string
}

// Attachment is a file stored against a ledger payment.
type Attachment struct {
	ID           int64
	PaymentID    int64
	OriginalName string
	StoredPath   string
	MimeType     string
	SizeBytes    int64
	UploadedBy   int64
	CreatedAt    time.Time
}

// AttachmentInput contains attachment metadata after the application has
// safely persisted the uploaded file outside the database.
type AttachmentInput struct {
	OriginalName string
	StoredPath   string
	MimeType     string
	SizeBytes    int64
}

// BudgetInput is one head's amount on the budget form.
type BudgetInput struct {
	HeadID int64
	Amount int64
}

// MonthLock records who locked a month, when, and why.
type MonthLock struct {
	Month     string
	LockedBy  int64
	ActorName string
	LockedAt  time.Time
	Reason    string
}

// AuditEntry is one audit-log row as read back. BeforeJSON and AfterJSON are
// the stored snapshots ("" when none).
type AuditEntry struct {
	ID         int64
	ActorID    *int64
	ActorName  string
	Action     string
	EntityType string
	EntityID   *int64
	Summary    string
	BeforeJSON string
	AfterJSON  string
	IP         string
	CreatedAt  time.Time
}

// GridRow is one head on the budget-versus-actual grid. Status is one of
// "over", "under", "on-track", "unbudgeted", "not-paid" or "no-activity".
type GridRow struct {
	ProjectID       int64
	Project         string
	HeadID          int64
	Head            string
	Active          bool
	DueDay          string
	Budget          int64
	Actual          int64
	Variance        int64
	VariancePercent string
	Status          string
}

// ProjectTotal is the budget, actual and variance summed over a set of rows:
// one project, or the whole grid.
type ProjectTotal struct {
	ProjectID       int64
	Project         string
	Budget          int64
	Actual          int64
	Variance        int64
	VariancePercent string
}

// GridData is the whole budget-versus-actual grid for one month.
type GridData struct {
	Month    string
	Rows     []GridRow
	Groups   []GridGroup
	Projects []ProjectTotal // each group's total, in the same order
	Total    ProjectTotal
	// Filtered is true when a status or search filter is on; ScopeLabel then
	// reads "Filtered subtotal" instead of "Company total".
	Filtered   bool
	ScopeLabel string
	Locked     bool
	Lock       MonthLock
	// Row counts by status, for the summary chips.
	Over       int
	Under      int
	OnTrack    int
	Unbudgeted int
	NotPaid    int
	NoActivity int
}

// GridGroup is one project's rows on the grid with their total.
type GridGroup struct {
	ProjectID int64
	Project   string
	Rows      []GridRow
	Total     ProjectTotal
}

// ReportRow is one line of the budget report: a head, a project or a whole
// month, depending on the report mode.
type ReportRow struct {
	Period          string
	ProjectID       int64
	HeadID          int64
	Project         string
	Head            string
	Budget          int64
	Actual          int64
	Variance        int64
	VariancePercent string
}

// RecoverableRow is one line of the outstanding-recoverables register. Ageing
// fields are computed against RecoverableReportOptions.AsOf, never time.Now(),
// so both the screens and their tests are deterministic.
type RecoverableRow struct {
	RequestID          int64
	Number             string
	Category           string
	Counterparty       string
	Project            string
	Amount             int64
	PaidAmount         int64
	RecoveredAmount    int64
	PaidOn             string // "" when approved but not yet paid
	ExpectedReturnDate string
	RepaymentNotes     string
	Status             string
	Requester          string
	OnHold             bool
	// ProcessingBy, ProcessingByName and ConcernOpen are what the status pill
	// turns on beyond the status itself — who holds the reservation, and
	// whether a partial review has an unanswered concern — so the register
	// reads "With Accounts — taken by …" and "Partial — under discussion"
	// exactly as the request behind it does.
	ProcessingBy     *int64
	ProcessingByName string
	ConcernOpen      bool
	HasReturnDate    bool
	Overdue          bool
	DaysToReturn     int    // whole calendar days from AsOf; negative when overdue
	AgeingLabel      string // "25 days overdue", "174 days to go", "Awaiting payment", …
	AgeingTone       string // pill modifier: "bad" | "neutral" | "approved" | "hold"
}

// RecoverableReportOptions filters and orders the recoverables register.
type RecoverableReportOptions struct {
	From         string // optional YYYY-MM; filters on paid_on
	To           string // optional YYYY-MM
	CategoryID   int64
	Counterparty string
	Query        string
	Ageing       string    // "" | "outstanding" | "overdue" | "due30" | "later" | "unpaid" | "recovered"
	Order        string    // "" (overdue first, then soonest return) | "amount" | "paid_on" | "number"
	AsOf         time.Time // zero → time.Now().UTC() at normalisation
	Viewer       RecoverableViewer
}

// RecoverableViewer is the caller a recoverable read is answered for.
//
// It exists as a named type rather than two loose fields because every one of the
// four readers over `payment_requests`-as-recoverables needs it and none of them
// can be trusted to default: the register is a second view over the requests
// table, so a read that forgets the viewer discloses every counterparty, amount
// and repayment note in the company.
//
// **The zero value sees nothing.** That is deliberate and it is the whole point of
// the type. `permissions.go` already documents an empty data scope as "no scope at
// all", and RequestInScope returns false for it, so denying matches both. It
// also fails in the safe direction: a caller who forgets to set Viewer gets an
// empty screen, which someone reports, rather than a silent disclosure, which
// nobody does.
type RecoverableViewer struct {
	// Scope is the caller's `request` data scope: ScopeAll, ScopeAssigned,
	// ScopeOwn, or "" for none. Narrower than the holder's real scope is allowed —
	// that is how a URL filter works — but the caller must never widen it here.
	Scope string
	// ViewerID is the reader's user id, consulted for "own" and "assigned".
	ViewerID int64
}

// RecoverableViewerAll reads as an administrator: every row, no filter. Named so
// that a caller intending "all" says so, and so that grepping for it finds every
// place the scope is deliberately bypassed.
func RecoverableViewerAll() RecoverableViewer { return RecoverableViewer{Scope: ScopeAll} }

// RecoverableMetrics is the four-number strip on the recoverables dashboard.
// Outstanding counts paid balances still owed; PaidThisMonth counts
// only money that actually left in the calendar month containing asOf.
type RecoverableMetrics struct {
	OutstandingAmount   int64
	OutstandingCount    int
	OverdueAmount       int64
	OverdueCount        int
	DueIn30Amount       int64
	DueIn30Count        int
	PaidThisMonthAmount int64
	PaidThisMonthCount  int
}

// RecoverableRollup is one grouped row of the dashboard's by-category or
// by-counterparty table. Detail is empty for category rows and lists the
// distinct categories for counterparty rows — the replacement for the mockup's
// unmodelled "Type" column.
type RecoverableRollup struct {
	Label        string
	Detail       string
	Count        int
	Outstanding  int64
	Overdue      int64
	Oldest       string // earliest paid_on in the group; "" when nothing is paid yet
	ExpectedBack string // earliest expected_return_date in the group
}

// PaymentInput is a payment as submitted by a form.
type PaymentInput struct {
	HeadID      int64
	PaidOn      string
	Amount      int64
	VendorPayee string
	PaymentMode string
	InvoiceNo   string
	ReferenceNo string
	Remarks     string
	// ExpectedPaid is the amount already paid that the form displayed; a
	// payment against a request is refused when it no longer matches, so a
	// stale installment form cannot overpay.
	ExpectedPaid *int64
	// SubmissionKey is a one-time key from the browser form, so a retried
	// submit cannot record the same installment twice.
	SubmissionKey string
	// Now is the clock PaidOn is checked against: a payment dated after Now's
	// day (server local time) is refused, because money cannot have left the
	// bank in the future. Zero means time.Now(). Tests set it to pin the day.
	Now time.Time
}

// PaymentListOptions filters the payment ledger.
type PaymentListOptions struct {
	Month     string
	ProjectID int64
	HeadID    int64
	VendorID  int64
	Query     string
	Status    string // "" = not voided | "voided" | "all"
	Limit     int
	Offset    int
	// Scope and ViewerID apply the viewer's payment data scope. "own" and
	// "assigned" (which has no routed-to meaning on the ledger) both keep the
	// payments the viewer entered; only ScopeAll leaves the list unrestricted.
	// "" and any other value read no rows, so a role with payment:view and no
	// scope sees an empty ledger, never every payment.
	Scope    string
	ViewerID int64
	// ExcludeRecoverable leaves out payments that settle a recoverable request.
	// The variance grid sets it: a recoverable is a deposit, not budget spend.
	ExcludeRecoverable bool
}

// Request is one payment request with the names its screens show joined in.
type Request struct {
	ID                        int64
	Number                    string
	Status                    string
	Treatment                 string
	Type                      string
	RecoverableCategory       string // code: emd|pbg|icd|employee_advance|security_deposit|other
	RecoverableCategoryID     *int64
	ProjectID                 *int64
	Project                   string // joined name; retained even when project inactive
	HeadID                    *int64
	Head                      string // joined name; retained even when head inactive
	VendorID                  *int64
	Vendor                    string // COALESCE(vendors.name, vendor_payee) — the display payee
	VendorGSTIN               string
	VendorPayee               string // snapshot; the only payee for reimbursement / employee advance
	ShortTitle                string
	Amount                    int64
	Purpose                   string
	NeededBy                  string // YYYY-MM-DD, "" if unset
	InvoiceNo                 string
	InvoiceDate               string // YYYY-MM-DD, "" if unset
	ExpenseDate               string // YYYY-MM-DD, "" if unset
	AdvanceReason             string
	Counterparty              string
	ExpectedReturnDate        string // YYYY-MM-DD, "" if unset
	RepaymentNotes            string
	Urgent                    bool
	UrgencyReason             string
	AttachmentExceptionReason string
	RequesterID               int64
	RequesterName             string
	ManagerID                 int64
	ManagerName               string
	ApprovedAmount            *int64
	ApprovedBy                *int64
	ApprovedByName            string
	ApprovedAt                *time.Time
	DecisionReason            string
	CancelReason              string
	OnHold                    bool
	HoldReason                string
	ProcessingBy              *int64
	ProcessingAt              *time.Time
	ProcessingByName          string // joined name of the reserver; "" when unreserved
	// ConcernOpen is set by RaiseConcern and cleared when the accountant who
	// holds the request answers in the conversation. It is a flag, not a status
	// (the request stays 'partial_review'), exactly as on_hold is a flag on
	// 'approved': the display reads "Partial — under discussion" from it.
	ConcernOpen      bool
	PaidAmount       int64 // sum of the request's payments that are not voided
	ReminderLastSent *time.Time
	SubmittedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RequestInput is a request as submitted by the new-request or edit form.
type RequestInput struct {
	Treatment                 string
	Type                      string
	RecoverableCategory       string
	ProjectID                 int64
	HeadID                    int64
	VendorID                  int64
	VendorPayee               string
	ShortTitle                string
	Amount                    int64
	Purpose                   string
	NeededBy                  string
	InvoiceNo                 string
	InvoiceDate               string
	ExpenseDate               string
	AdvanceReason             string
	Counterparty              string
	ExpectedReturnDate        string
	RepaymentNotes            string
	Urgent                    bool
	UrgencyReason             string
	AttachmentExceptionReason string
	ManagerID                 int64
	// DuplicateReason explains why the requester went ahead although a similar
	// request exists.
	DuplicateReason string
	// RequesterID is filled by the store from the actor, never from the form,
	// so validateRequestInput can reject a requester approving their own request.
	RequesterID int64
	// Attachments are staged by the handler and written in the same transaction
	// as the request itself; there are no drafts, so there is no earlier moment
	// at which a file could be attached.
	Attachments []AttachmentInput
}

// RequestListOptions filters request lists.
type RequestListOptions struct {
	// Scope is the viewer's request data scope (ScopeOwn, ScopeAssigned or
	// ScopeAll, see request_scope.go). It says which requests the viewer may see
	// at all; a URL may narrow it and must never widen it. Anything else reads
	// no rows.
	Scope    string
	ViewerID int64
	// RoutedToViewer keeps only the requests whose approver is the viewer: the
	// approvals queue, its badge and the dashboard's "awaiting your approval"
	// area. It is a filter inside the scope, not a scope of its own, so an
	// approver's own raised requests never appear in their approval queue.
	RoutedToViewer bool
	// HeldByViewer keeps only the requests the viewer has reserved for payment:
	// the accountant's "concerns to answer" list. Also a filter inside the scope.
	HeldByViewer bool
	Status       string   // "" or "all" = any status; otherwise exact status
	Statuses     []string // optional explicit set; wins over Status when non-empty
	Bucket       string   // "" | "open" | "closed" | "needs-me" | "all"
	// ConcernOpen narrows to partial reviews whose concern Accounts has not
	// yet answered — the accountant's "concerns to answer" list.
	ConcernOpen bool
	Type        string
	Treatment   string
	ProjectID   int64
	VendorID    int64
	Query       string
	Limit       int
}

// StaleReservation is how long a reservation may sit before the queue nudges.
// Nothing is ever released automatically — a bank transfer may be under way.
const StaleReservation = 24 * time.Hour

// LinkableOptions selects rows for the Accounts queue and the payment picker.
type LinkableOptions struct {
	Scope    string // the viewer's request data scope (ScopeOwn | ScopeAssigned | ScopeAll), from auth.Scope
	ViewerID int64
	Status   string // "" (picker pool, including unavailable) | "approved" (unclaimed, not held) | "processing" | "hold" | "partial_review" | "paid"
	Query    string
	Limit    int
	Now      time.Time // injected clock; zero means time.Now()
}

// LinkableCounts feeds the queue's .segmented tabs and .metric-strip. It is
// always computed over the caller's whole scope, never over the current page or
// the current search, so the tab numbers do not move as you type.
type LinkableCounts struct {
	Approved          int
	Processing        int
	Hold              int
	PartialReview     int
	Paid              int
	ReservedByMe      int
	ReservedByOthers  int
	StaleReservations int
	ApprovedAmount    int64
}

// LinkableSet splits what the caller may act on from what they may only see.
// Unavailable is deliberately not hidden: the picker renders it as .co.is-taken
// so an accountant learns the request exists and who holds it.
type LinkableSet struct {
	Available   []Request
	Unavailable []Request
	Counts      LinkableCounts
}

// RequestComment is one message in a request's conversation.
type RequestComment struct {
	ID         int64
	RequestID  int64
	AuthorID   int64
	AuthorName string
	Body       string
	CreatedAt  time.Time
}

// RequestAttachment is a file stored against a request (not a payment).
type RequestAttachment struct {
	ID           int64
	RequestID    int64
	OriginalName string
	StoredPath   string
	MimeType     string
	SizeBytes    int64
	UploadedBy   int64
	CreatedAt    time.Time
}

// ThreadEntry is one line of the merged history-and-conversation stream that
// `.thread` renders. Events, comments and attachments are one chronological
// list, not three.
type ThreadEntry struct {
	Kind      string // "event" | "comment" | "attachment"
	Action    string // audit action for events; "" for comments
	ActorID   int64
	ActorName string
	Initials  string
	Title     string
	Body      string
	FileName  string
	FileSize  int64
	Changes   []ThreadChange
	CreatedAt time.Time
}

// ThreadChange is one field an event changed, shown as "was → now".
type ThreadChange struct {
	Field string
	Was   string
	Now   string
}

// AuditInput is one audit-log row to write. Before and After are stored as JSON.
// Build it with auditBy inside the store.
type AuditInput struct {
	ActorID    *int64
	ActorName  string
	Action     string
	EntityType string
	EntityID   *int64
	Summary    string
	Before     any
	After      any
	IP         string
}

// SimilarRequestOptions drives the duplicate check. It is advisory: the result
// is shown to the person and never gates the submit.
type SimilarRequestOptions struct {
	ExcludeID int64
	VendorID  int64
	Payee     string
	Amount    int64
	InvoiceNo string
	Days      int // default 30
	Limit     int // default 5
}
