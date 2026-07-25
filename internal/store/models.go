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

type PaymentInput struct {
	HeadID      int64
	PaidOn      string
	Amount      int64
	VendorPayee string
	PaymentMode string
	InvoiceNo   string
	ReferenceNo string
	Remarks     string
}

type PaymentListOptions struct {
	Month  string
	Query  string
	Status string
	Limit  int
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
