package store

import (
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrLockedMonth  = errors.New("month is locked")
	ErrValidation   = errors.New("validation failed")
	ErrDuplicate    = errors.New("duplicate value")
	ErrInactiveHead = errors.New("project or head is inactive")
)

type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	Role         string
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// DefaultApproverID is who approves this user's requests unless the request
	// says otherwise (G9); 0 means none. A user is never their own default —
	// SetUserDefaultApprover enforces it.
	DefaultApproverID int64
}

type Project struct {
	ID        int64
	Name      string
	Active    bool
	SortOrder int
	CreatedAt time.Time
}

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
// Code is the stable identity and never changes once created: Phase 2 stores it
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

type Budget struct {
	ID        int64
	HeadID    int64
	Month     string
	Amount    int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

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
	// Phase 3 linkage. RequestID is nil for every historical payment, which is
	// what keeps those rows editable and voidable (X6); a non-nil RequestID
	// makes the payment immutable (S12).
	RequestID     *int64
	Settlement    string // "" (historical) | "settled" | "partial"
	PartialReason string
	// Treatment is the linked request's treatment ("" for a historical payment).
	// A "recoverable" payment has no head (v8), so a screen reads this to name it
	// rather than print an empty "Project / Head" (recoverables-2).
	Treatment string
}

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

type BudgetInput struct {
	HeadID int64
	Amount int64
}

type MonthLock struct {
	Month     string
	LockedBy  int64
	ActorName string
	LockedAt  time.Time
	Reason    string
}

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

type ProjectTotal struct {
	ProjectID       int64
	Project         string
	Budget          int64
	Actual          int64
	Variance        int64
	VariancePercent string
}

type GridData struct {
	Month      string
	Rows       []GridRow
	Projects   []ProjectTotal
	Groups     []GridGroup
	Total      ProjectTotal
	Locked     bool
	Lock       MonthLock
	Over       int
	Unbudgeted int
	Under      int
	OnTrack    int
	NotPaid    int
}

type GridGroup struct {
	ProjectID int64
	Project   string
	Rows      []GridRow
	Total     ProjectTotal
}

type ReportRow struct {
	Period          string
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
	PaidOn             string // "" when approved but not yet paid
	ExpectedReturnDate string
	RepaymentNotes     string
	Status             string
	Requester          string
	OnHold             bool
	HasReturnDate      bool
	Overdue            bool
	DaysToReturn       int    // whole calendar days from AsOf; negative when overdue
	AgeingLabel        string // "25 days overdue", "174 days to go", "Awaiting payment", …
	AgeingTone         string // pill modifier: "bad" | "neutral" | "approved" | "hold"
}

type RecoverableReportOptions struct {
	From         string // optional YYYY-MM; filters on paid_on
	To           string // optional YYYY-MM
	CategoryID   int64
	Counterparty string
	Query        string
	Ageing       string    // "" | "overdue" | "due30" | "later" | "unpaid"
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
// and repayment note in the company (F-G-016/F-E-03).
//
// **The zero value sees nothing.** That is deliberate and it is the whole point of
// the type. `permissions.go` already documents an empty data scope as "no scope at
// all", and `canViewRequest` returns false for it, so denying matches both. It
// also fails in the safe direction: a caller who forgets to set Viewer gets an
// empty screen, which someone reports, rather than a silent disclosure, which
// nobody does.
type RecoverableViewer struct {
	// Scope is the caller's `request` data scope: ScopeAll, "assigned", "own", or
	// "" for none. Narrower than the holder's real scope is allowed — that is how
	// a URL filter works — but the caller must never widen it here.
	Scope string
	// ViewerID is the reader's user id, consulted for "own" and "assigned".
	ViewerID int64
}

// RecoverableViewerAll reads as an administrator: every row, no filter. Named so
// that a caller intending "all" says so, and so that grepping for it finds every
// place the scope is deliberately bypassed.
func RecoverableViewerAll() RecoverableViewer { return RecoverableViewer{Scope: ScopeAll} }

// RecoverableMetrics is the four-number strip on the recoverables dashboard.
// Outstanding counts every live recoverable, paid or not; PaidThisMonth counts
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

type PaymentInput struct {
	HeadID      int64
	PaidOn      string
	Amount      int64
	VendorPayee string
	PaymentMode string
	InvoiceNo   string
	ReferenceNo string
	Remarks     string
	// Now is the injected clock validatePayment measures PaidOn against
	// (F-D-06): a payment dated after Now's date is refused, because money
	// cannot have left the bank in the future. Zero means "no clock supplied",
	// and the check then measures against time.Now().UTC() — the default is
	// enforcement, never a skip, because a rule that switches itself off when a
	// caller forgets to pass a clock is not a rule. Injection is for tests that
	// need to pin the day. Mirrors LinkableOptions.Now.
	Now time.Time
}

type PaymentListOptions struct {
	Month  string
	Query  string
	Status string
	Limit  int
	// Scope + ViewerID enforce the `payment` data scope (F-A-04 / F-G-003),
	// mirroring RequestListOptions: "own" (and "assigned", which has no
	// routed-to meaning on the ledger and narrows the same way rather than
	// silently widening) filter on entered_by = ViewerID; "", "all" and any
	// other value leave the list unrestricted, exactly as requestWhere does.
	Scope    string
	ViewerID int64
	// ExcludeRecoverable leaves out payments that settle a recoverable request.
	// The variance grid sets it: a recoverable is a deposit, not budget spend,
	// and its totals already leave them out (V2, recoverables-2).
	ExcludeRecoverable bool
}

type Request struct {
	ID                        int64
	Number                    string
	Status                    string
	Treatment                 string
	Type                      string
	RecoverableCategory       string // code: emd|pbg|icd|employee_advance|security_deposit|other
	RecoverableCategoryID     *int64 // linked by Phase 4
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
	ReminderLastSent          *time.Time
	SubmittedAt               *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

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
	// RequesterID is filled by the store from the actor, never from the form.
	// It exists so validateRequestInput can reject self-approval (G8).
	RequesterID int64
	// Attachments are staged by the handler and written in the same transaction
	// as the request itself. D1 removed drafts, so there is no earlier moment at
	// which a file could be attached.
	Attachments []AttachmentInput
}

type RequestListOptions struct {
	Scope     string // "own" | "assigned" | "all"
	ViewerID  int64
	Status    string   // "" or "all" = any status; otherwise exact status
	Statuses  []string // optional explicit set; wins over Status when non-empty
	Bucket    string   // "" | "open" | "closed" | "needs-me" | "all"
	Type      string
	Treatment string
	ProjectID int64
	Query     string
	Limit     int
}

// StaleReservation is how long a reservation may sit before the queue nudges.
// Nothing is ever released automatically — a bank transfer may be under way.
const StaleReservation = 24 * time.Hour

type LinkableOptions struct {
	Scope    string // "own" | "assigned" | "all", from auth.Scope
	ViewerID int64
	Status   string // "" or "approved" (the approved pool) | "processing" | "hold" | "partial_review" | "paid"
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

type RequestComment struct {
	ID         int64
	RequestID  int64
	AuthorID   int64
	AuthorName string
	Body       string
	CreatedAt  time.Time
}

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
// list, not three (UI/UX §9; request-detail-employee.html).
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

type ThreadChange struct {
	Field string
	Was   string
	Now   string
}

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
// is shown to the person and never gates the submit (G6).
type SimilarRequestOptions struct {
	ExcludeID int64
	VendorID  int64
	Payee     string
	Amount    int64
	InvoiceNo string
	Days      int // default 30
	Limit     int // default 5
}
