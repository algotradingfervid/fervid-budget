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
	Month    string
	Rows     []GridRow
	Projects []ProjectTotal
	Groups   []GridGroup
	Total    ProjectTotal
	Locked   bool
	Lock     MonthLock
	Over     int
	Under    int
	OnTrack  int
	NotPaid  int
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
