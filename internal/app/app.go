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
	"fervidbudget/internal/store"
)

type App struct {
	cfg  config.Config
	st   *store.Store
	auth *auth.Manager
	tpl  *template.Template
	log  *slog.Logger
}

type PageData struct {
	Title string
	User  store.User
	Shell Shell
	// Perms is how a template asks whether the signed-in user may do something.
	// No template compares a role name: every gated control names the resource
	// and action its route is guarded by, and the permission set answers. It is
	// always non-nil, so a signed-out render simply gates everything off.
	Perms          store.PermissionSet
	CSRF           string
	Error          string
	Notice         string
	Month          string
	Status         string
	Query          string
	Grid           store.GridData
	Projects       []store.Project
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
	am.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, status int, message string) {
		a.respondError(w, r, status, message, nil)
	})
	a.tpl = template.Must(template.New("base").Funcs(template.FuncMap{
		"money": money.FormatPaise,
		"short": money.FormatShort,
		"date":  func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"datep": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
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
	mux.HandleFunc("POST /login", a.loginPost)
	mux.HandleFunc("POST /logout", a.withCSRF(a.logoutPost))
	mux.Handle("GET /", a.auth.RequireLogin(http.HandlerFunc(a.grid)))
	mux.Handle("GET /grid", a.auth.RequireLogin(http.HandlerFunc(a.grid)))
	mux.Handle("GET /months", a.auth.RequirePermission("month", "view", http.HandlerFunc(a.months)))
	mux.Handle("POST /months", a.auth.RequirePermission("month", "create", http.HandlerFunc(a.withCSRF(a.monthCreate))))
	mux.Handle("GET /payments/new", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentForm)))
	mux.Handle("GET /payments", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.payments)))
	mux.Handle("POST /payments", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.paymentCreate))))
	mux.Handle("GET /payments/{id}", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.paymentDetail)))
	mux.Handle("GET /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.paymentEditForm)))
	mux.Handle("POST /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.withCSRF(a.paymentEdit))))
	mux.Handle("POST /payments/{id}/void", a.auth.RequirePermission("payment", "void", http.HandlerFunc(a.withCSRF(a.paymentVoid))))
	mux.Handle("POST /payments/{id}/attachments", a.auth.RequirePermission("attachment", "create", http.HandlerFunc(a.withCSRF(a.attachmentUpload))))
	mux.Handle("GET /attachments/{id}", a.auth.RequirePermission("attachment", "view", http.HandlerFunc(a.attachmentDownload)))
	mux.Handle("GET /export.csv", a.auth.RequirePermission("grid", "export", http.HandlerFunc(a.exportGrid)))
	mux.Handle("GET /reports/monthly", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/projects", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/heads", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/ytd.csv", a.auth.RequirePermission("report", "export", http.HandlerFunc(a.exportYTD)))
	mux.Handle("GET /budgets", a.auth.RequirePermission("budget", "view", http.HandlerFunc(a.budgets)))
	mux.Handle("POST /budgets", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.withCSRF(a.budgetSave))))
	mux.Handle("POST /months/{month}/lock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.lockMonth))))
	mux.Handle("POST /months/{month}/unlock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.unlockMonth))))
	mux.Handle("GET /projects", a.auth.RequirePermission("project", "view", http.HandlerFunc(a.projects)))
	mux.Handle("POST /projects", a.auth.RequirePermission("project", "edit", http.HandlerFunc(a.withCSRF(a.projectSave))))
	mux.Handle("GET /heads", a.auth.RequirePermission("head", "view", http.HandlerFunc(a.heads)))
	mux.Handle("POST /heads", a.auth.RequirePermission("head", "edit", http.HandlerFunc(a.withCSRF(a.headSave))))
	mux.Handle("GET /users", a.auth.RequirePermission("user", "view", http.HandlerFunc(a.users)))
	mux.Handle("POST /users", a.auth.RequirePermission("user", "edit", http.HandlerFunc(a.withCSRF(a.userSave))))
	mux.Handle("GET /roles", a.auth.RequirePermission("role", "view", http.HandlerFunc(a.rolesPage)))
	mux.Handle("POST /roles", a.auth.RequirePermission("role", "edit", http.HandlerFunc(a.withCSRF(a.rolesSave))))
	mux.Handle("POST /roles/new", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCreate))))
	mux.Handle("POST /roles/{id}/copy", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCopy))))
	mux.Handle("POST /roles/{id}/delete", a.auth.RequirePermission("role", "delete", http.HandlerFunc(a.withCSRF(a.roleDelete))))
	mux.Handle("GET /audit", a.auth.RequirePermission("audit", "view", http.HandlerFunc(a.auditLog)))
	mux.Handle("GET /backups", a.auth.RequirePermission("backup", "view", http.HandlerFunc(a.backups)))
	mux.Handle("POST /backups", a.auth.RequirePermission("backup", "create", http.HandlerFunc(a.withCSRF(a.backupCreate))))
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	a.renderStatus(w, r, http.StatusOK, name, data)
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
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Month: month, Status: "active", Limit: 10})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "grid", PageData{Title: "Variance Grid", Grid: grid, CloseGrid: closeGrid, Month: month, Status: status, Query: q, Payments: payments})
}

func (a *App) paymentForm(w http.ResponseWriter, r *http.Request) {
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	selectedHeadID := parseID(r.URL.Query().Get("head_id"))
	paidOn := time.Now().Format("2006-01-02")
	if date := r.URL.Query().Get("date"); validDateInput(date) {
		paidOn = date
	} else if month := r.URL.Query().Get("month"); validMonthInput(month) {
		paidOn = month + "-01"
	}
	locked := a.st.IsLocked(r.Context(), paidOn[:7])
	a.render(w, r, "payment_form", PageData{Title: "Add Payment", Heads: heads, SelectedHeadID: selectedHeadID, Payment: store.Payment{HeadID: selectedHeadID, PaidOn: paidOn}, Locked: locked})
}

func (a *App) payments(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := queryDefault(r, "status", "active")
	q := r.URL.Query().Get("q")
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Month: month, Status: status, Query: q})
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

func (a *App) paymentCreate(w http.ResponseWriter, r *http.Request) {
	in, err := paymentInput(r)
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	if err == nil {
		_, err = a.st.CreatePaymentWithAttachment(r.Context(), auth.CurrentUser(r), in, attachment)
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
		paidOn := in.PaidOn
		locked := validDateInput(paidOn) && a.st.IsLocked(r.Context(), paidOn[:7])
		a.renderStatus(w, r, status, "payment_form", PageData{Title: "Add Payment", Heads: heads, SelectedHeadID: in.HeadID, Payment: paymentFromInput(in), PaymentAmount: r.FormValue("amount"), Locked: locked, Error: friendly(err)})
		return
	}
	if r.FormValue("submit_action") == "add_another" {
		http.Redirect(w, r, fmt.Sprintf("/payments/new?date=%s&head_id=%d", in.PaidOn, in.HeadID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?month="+in.PaidOn[:7], http.StatusSeeOther)
}

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
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "payment_form", PageData{Title: "Edit Payment", Payment: p, Heads: heads, Locked: a.st.IsLocked(r.Context(), p.PaidOn[:7]) || p.VoidedAt != nil})
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
		a.renderStatus(w, r, status, "payment_form", PageData{Title: "Edit Payment", Payment: p, PaymentAmount: r.FormValue("amount"), Heads: heads, Locked: locked, Error: friendly(err)})
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

func (a *App) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
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

func (a *App) attachmentDownload(w http.ResponseWriter, r *http.Request) {
	att, err := a.st.AttachmentByID(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	root, err := filepath.Abs(a.cfg.AttachmentDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The attachment could not be opened.", err)
		return
	}
	stored, err := filepath.Abs(att.StoredPath)
	if err != nil || !strings.HasPrefix(stored, root+string(os.PathSeparator)) {
		a.log.WarnContext(r.Context(), "unsafe attachment path rejected", "request_id", requestID(r), "attachment_id", att.ID)
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
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.OriginalName}))
	if att.MimeType != "" {
		w.Header().Set("Content-Type", att.MimeType)
	}
	http.ServeFile(w, r, stored)
}

func (a *App) stageUploadedAttachment(r *http.Request) (*store.AttachmentInput, string, error) {
	file, header, err := r.FormFile("attachment")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
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
		a.renderStatus(w, r, http.StatusBadRequest, "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source, Error: friendly(err)})
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
	var updates []store.BudgetInput
	var parseErr error
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "budget_") {
			continue
		}
		headID, idErr := strconv.ParseInt(strings.TrimPrefix(k, "budget_"), 10, 64)
		raw := ""
		if len(v) > 0 {
			raw = v[0]
		}
		inputs[headID] = raw
		amt, amountErr := money.ParsePaise(raw)
		if idErr != nil || headID <= 0 || amountErr != nil {
			parseErr = fmt.Errorf("%w: invalid budget amount for one or more heads", store.ErrValidation)
			continue
		}
		updates = append(updates, store.BudgetInput{HeadID: headID, Amount: amt})
	}
	if parseErr == nil {
		parseErr = a.st.SetBudgets(r.Context(), u, month, updates)
	}
	if parseErr != nil {
		grid, gridErr := a.st.Grid(r.Context(), month, "", "")
		heads, headsErr := a.st.ListHeads(r.Context(), true)
		if gridErr != nil || headsErr != nil {
			a.respondStoreError(w, r, errors.Join(gridErr, headsErr))
			return
		}
		a.renderStatus(w, r, http.StatusBadRequest, "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads, BudgetInputs: inputs, Error: friendly(parseErr)})
		return
	}
	http.Redirect(w, r, "/budgets?month="+month, http.StatusSeeOther)
}

func (a *App) lockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.LockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
}

func (a *App) unlockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.UnlockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
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
	heads, err := a.st.ListHeads(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	projects, err := a.st.ListProjects(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "heads", PageData{Title: "Heads", Heads: heads, Projects: projects})
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
	a.render(w, r, "users", PageData{Title: "Users", Users: users})
}

func (a *App) userSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
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
		err = a.st.UpdateUser(r.Context(), id, r.FormValue("name"), r.FormValue("role"), active, hash)
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
	if len(audit) > 200 {
		audit = audit[:200]
	}
	a.render(w, r, "audit", PageData{Title: "Audit Log", Audit: audit, AuditEntity: entity, AuditAction: action, AuditActor: r.URL.Query().Get("actor")})
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
		_ = cw.Write([]string{row.Project, row.Head, money.FormatPaise(row.Budget), money.FormatPaise(row.Actual), money.FormatPaise(row.Variance), row.VariancePercent, row.Status})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="variance-`+month+`.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

func (a *App) exportYTD(w http.ResponseWriter, r *http.Request) {
	year := queryDefault(r, "year", time.Now().Format("2006"))
	from := validMonthOrFallback(r.URL.Query().Get("from"), year+"-01")
	to := validMonthOrFallback(r.URL.Query().Get("to"), year+"-12")
	if from > to {
		from, to = to, from
	}
	rows, err := a.st.Report(r.Context(), from, to, "heads")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported report " + from + " to " + to})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Period", "Project", "Head", "Budget", "Actual", "Variance", "Variance %"})
	for _, row := range rows {
		_ = cw.Write([]string{row.Period, row.Project, row.Head, money.FormatPaise(row.Budget), money.FormatPaise(row.Actual), money.FormatPaise(row.Variance), row.VariancePercent})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
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

func validatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	return nil
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

func friendly(err error) string {
	switch {
	case errors.Is(err, store.ErrLockedMonth):
		return "This month is locked. Unlock it with a reason before changing budgets or payments."
	case errors.Is(err, store.ErrDuplicate):
		return "A record with this name already exists."
	case errors.Is(err, store.ErrInactiveHead):
		return "This project/head is inactive."
	case errors.Is(err, store.ErrValidation):
		return err.Error()
	default:
		return "Something went wrong while processing your request."
	}
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

func boolText(v bool) string {
	if v {
		return "Active"
	}
	return "Inactive"
}

func actionText(action string) string {
	switch action {
	case "create":
		return "Created"
	case "update":
		return "Updated"
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
	default:
		return action
	}
}

func actionClass(action string) string {
	switch action {
	case "void", "login_failed":
		return "danger"
	case "lock", "unlock":
		return "warn"
	case "create":
		return "good"
	case "update":
		return "info"
	default:
		return "neutral"
	}
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
