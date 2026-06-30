package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
}

type PageData struct {
	Title          string
	User           store.User
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
	a := &App{cfg: cfg, st: st, auth: am}
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
		"planStatusText": planStatusText,
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
	return &http.Server{Addr: cfg.Addr, Handler: am.Middleware(mux)}, nil
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
	mux.Handle("GET /months", a.auth.RequirePermission("budget", "read", http.HandlerFunc(a.months)))
	mux.Handle("POST /months", a.auth.RequirePermission("budget", "write", http.HandlerFunc(a.withCSRF(a.monthCreate))))
	mux.Handle("GET /payments/new", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentForm)))
	mux.Handle("GET /payments", a.auth.RequirePermission("payment", "read", http.HandlerFunc(a.payments)))
	mux.Handle("POST /payments", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.paymentCreate))))
	mux.Handle("GET /payments/{id}", a.auth.RequirePermission("payment", "read", http.HandlerFunc(a.paymentDetail)))
	mux.Handle("GET /payments/{id}/edit", a.auth.RequirePermission("payment", "update", http.HandlerFunc(a.paymentEditForm)))
	mux.Handle("POST /payments/{id}/edit", a.auth.RequirePermission("payment", "update", http.HandlerFunc(a.withCSRF(a.paymentEdit))))
	mux.Handle("POST /payments/{id}/void", a.auth.RequirePermission("payment", "void", http.HandlerFunc(a.withCSRF(a.paymentVoid))))
	mux.Handle("POST /payments/{id}/attachments", a.auth.RequirePermission("payment_attachment", "create", http.HandlerFunc(a.withCSRF(a.attachmentUpload))))
	mux.Handle("GET /export.csv", a.auth.RequirePermission("report", "export", http.HandlerFunc(a.exportGrid)))
	mux.Handle("GET /reports/monthly", a.auth.RequirePermission("report", "read", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/projects", a.auth.RequirePermission("report", "read", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/heads", a.auth.RequirePermission("report", "read", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/ytd.csv", a.auth.RequirePermission("report", "export", http.HandlerFunc(a.exportYTD)))
	mux.Handle("GET /budgets", a.auth.RequirePermission("budget", "read", http.HandlerFunc(a.budgets)))
	mux.Handle("POST /budgets", a.auth.RequirePermission("budget", "write", http.HandlerFunc(a.withCSRF(a.budgetSave))))
	mux.Handle("POST /months/{month}/lock", a.auth.RequirePermission("month_lock", "write", http.HandlerFunc(a.withCSRF(a.lockMonth))))
	mux.Handle("POST /months/{month}/unlock", a.auth.RequirePermission("month_lock", "write", http.HandlerFunc(a.withCSRF(a.unlockMonth))))
	mux.Handle("GET /projects", a.auth.RequirePermission("project", "read", http.HandlerFunc(a.projects)))
	mux.Handle("POST /projects", a.auth.RequirePermission("project", "write", http.HandlerFunc(a.projectSave)))
	mux.Handle("GET /heads", a.auth.RequirePermission("head", "read", http.HandlerFunc(a.heads)))
	mux.Handle("POST /heads", a.auth.RequirePermission("head", "write", http.HandlerFunc(a.headSave)))
	mux.Handle("GET /users", a.auth.RequirePermission("user", "read", http.HandlerFunc(a.users)))
	mux.Handle("POST /users", a.auth.RequirePermission("user", "write", http.HandlerFunc(a.userSave)))
	mux.Handle("GET /audit", a.auth.RequirePermission("audit", "read", http.HandlerFunc(a.auditLog)))
	mux.Handle("GET /backups", a.auth.RequirePermission("backup", "read", http.HandlerFunc(a.backups)))
	mux.Handle("POST /backups", a.auth.RequirePermission("backup", "create", http.HandlerFunc(a.withCSRF(a.backupCreate))))
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	data.User = auth.CurrentUser(r)
	data.CSRF = a.auth.EnsureCSRF(w, r)
	if data.Month == "" {
		data.Month = time.Now().Format("2006-01")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (a *App) withCSRF(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.auth.CheckCSRF(r) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
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
		a.render(w, r, "login", PageData{Title: "Login", Error: "Invalid form"})
		return
	}
	u, err := a.st.UserByEmail(r.Context(), r.FormValue("email"))
	if err != nil || !u.Active || !auth.CheckPassword(u.PasswordHash, r.FormValue("password")) {
		a.render(w, r, "login", PageData{Title: "Login", Error: "Invalid email or password"})
		return
	}
	a.auth.Login(w, r, u)
	_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "login", EntityType: "user", EntityID: &u.ID, Summary: "Logged in", IP: r.RemoteAddr})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) logoutPost(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	if u.ID != 0 {
		_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "logout", EntityType: "user", EntityID: &u.ID, Summary: "Logged out", IP: r.RemoteAddr})
	}
	a.auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) grid(w http.ResponseWriter, r *http.Request) {
	month := queryDefault(r, "month", time.Now().Format("2006-01"))
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	grid, err := a.st.Grid(r.Context(), month, status, q)
	if err != nil {
		a.render(w, r, "grid", PageData{Title: "Variance Grid", Error: err.Error(), Month: month})
		return
	}
	payments, _ := a.st.RecentPayments(r.Context(), 10)
	a.render(w, r, "grid", PageData{Title: "Variance Grid", Grid: grid, Month: month, Status: status, Query: q, Payments: payments})
}

func (a *App) paymentForm(w http.ResponseWriter, r *http.Request) {
	heads, _ := a.st.ListHeads(r.Context(), true)
	selectedHeadID := parseID(r.URL.Query().Get("head_id"))
	paidOn := time.Now().Format("2006-01-02")
	if month := r.URL.Query().Get("month"); validMonthInput(month) {
		paidOn = month + "-01"
	}
	a.render(w, r, "payment_form", PageData{Title: "Add Payment", Heads: heads, SelectedHeadID: selectedHeadID, Payment: store.Payment{HeadID: selectedHeadID, PaidOn: paidOn}})
}

func (a *App) payments(w http.ResponseWriter, r *http.Request) {
	month := queryDefault(r, "month", time.Now().Format("2006-01"))
	status := queryDefault(r, "status", "active")
	q := r.URL.Query().Get("q")
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Month: month, Status: status, Query: q})
	if err != nil {
		a.render(w, r, "payments", PageData{Title: "Payments", Month: month, Status: status, Query: q, Error: friendly(err)})
		return
	}
	var total int64
	for _, payment := range payments {
		if payment.VoidedAt == nil {
			total += payment.Amount
		}
	}
	a.render(w, r, "payments", PageData{Title: "Payments", Month: month, Status: status, Query: q, Payments: payments, PaymentTotal: total})
}

func (a *App) paymentCreate(w http.ResponseWriter, r *http.Request) {
	in, err := paymentInput(r)
	var id int64
	if err == nil {
		id, err = a.st.CreatePayment(r.Context(), auth.CurrentUser(r), in)
	}
	if err == nil {
		err = a.saveUploadedAttachment(r, id)
	}
	if err != nil {
		heads, _ := a.st.ListHeads(r.Context(), true)
		a.render(w, r, "payment_form", PageData{Title: "Add Payment", Heads: heads, SelectedHeadID: in.HeadID, Payment: paymentFromInput(in), Error: friendly(err)})
		return
	}
	if r.FormValue("submit_action") == "add_another" {
		http.Redirect(w, r, fmt.Sprintf("/payments/new?month=%s&head_id=%d", in.PaidOn[:7], in.HeadID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?month="+in.PaidOn[:7], http.StatusSeeOther)
}

func (a *App) paymentDetail(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	p, err := a.st.Payment(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	atts, _ := a.st.Attachments(r.Context(), id)
	audit, _ := a.st.Audit(r.Context(), "payment", id, 50)
	a.render(w, r, "payment_detail", PageData{Title: "Payment Detail", Payment: p, Attachments: atts, Audit: audit})
}

func (a *App) paymentEditForm(w http.ResponseWriter, r *http.Request) {
	p, err := a.st.Payment(r.Context(), pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	heads, _ := a.st.ListHeads(r.Context(), true)
	a.render(w, r, "payment_form", PageData{Title: "Edit Payment", Payment: p, Heads: heads})
}

func (a *App) paymentEdit(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	in, err := paymentInput(r)
	if err == nil {
		err = a.st.UpdatePayment(r.Context(), auth.CurrentUser(r), id, in)
	}
	if err != nil {
		p, _ := a.st.Payment(r.Context(), id)
		heads, _ := a.st.ListHeads(r.Context(), true)
		a.render(w, r, "payment_form", PageData{Title: "Edit Payment", Payment: p, Heads: heads, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

func (a *App) paymentVoid(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	err := a.st.VoidPayment(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason"))
	if err != nil {
		http.Error(w, friendly(err), http.StatusBadRequest)
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
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "invalid upload", 400)
		return
	}
	if err := a.saveUploadedAttachment(r, id); err != nil {
		http.Error(w, friendly(err), 500)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

func (a *App) saveUploadedAttachment(r *http.Request, paymentID int64) error {
	file, header, err := r.FormFile("attachment")
	if err != nil {
		return nil
	}
	defer file.Close()
	if strings.TrimSpace(header.Filename) == "" {
		return nil
	}
	name := fmt.Sprintf("%d-%d-%s", paymentID, time.Now().UnixNano(), filepath.Base(header.Filename))
	stored := filepath.Join(a.cfg.AttachmentDir, name)
	out, err := os.Create(stored)
	if err != nil {
		return err
	}
	size, err := io.Copy(out, file)
	cerr := out.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	return a.st.AddAttachment(r.Context(), auth.CurrentUser(r), paymentID, header.Filename, stored, header.Header.Get("Content-Type"), size)
}

func (a *App) months(w http.ResponseWriter, r *http.Request) {
	plans, err := a.st.ListMonthPlans(r.Context())
	if err != nil {
		a.render(w, r, "months", PageData{Title: "Monthly Plans", Error: friendly(err)})
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
		plans, _ := a.st.ListMonthPlans(r.Context())
		a.render(w, r, "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, "/budgets?month="+target, http.StatusSeeOther)
}

func (a *App) budgets(w http.ResponseWriter, r *http.Request) {
	month := queryDefault(r, "month", time.Now().Format("2006-01"))
	grid, _ := a.st.Grid(r.Context(), month, "", "")
	heads, _ := a.st.ListHeads(r.Context(), true)
	a.render(w, r, "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads})
}

func (a *App) budgetSave(w http.ResponseWriter, r *http.Request) {
	month := r.FormValue("month")
	u := auth.CurrentUser(r)
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "budget_") {
			continue
		}
		headID, _ := strconv.ParseInt(strings.TrimPrefix(k, "budget_"), 10, 64)
		amt, err := money.ParsePaise(v[0])
		if err == nil {
			if err := a.st.SetBudget(r.Context(), u, headID, month, amt); err != nil {
				http.Error(w, friendly(err), 400)
				return
			}
		}
	}
	http.Redirect(w, r, "/budgets?month="+month, http.StatusSeeOther)
}

func (a *App) lockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.LockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		http.Error(w, friendly(err), 400)
		return
	}
	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
}

func (a *App) unlockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.UnlockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		http.Error(w, friendly(err), 400)
		return
	}
	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
}

func (a *App) projects(w http.ResponseWriter, r *http.Request) {
	projects, _ := a.st.ListProjects(r.Context(), false)
	a.render(w, r, "projects", PageData{Title: "Projects", Projects: projects})
}

func (a *App) projectSave(w http.ResponseWriter, r *http.Request) {
	if !a.auth.CheckCSRF(r) {
		http.Error(w, "invalid CSRF token", 403)
		return
	}
	id := parseID(r.FormValue("id"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	pid, err := a.st.UpsertProject(r.Context(), id, r.FormValue("name"), r.FormValue("active") == "on" || id == 0, sortOrder)
	if err == nil {
		u := auth.CurrentUser(r)
		_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "update", EntityType: "project", EntityID: &pid, Summary: "Saved project " + r.FormValue("name")})
	}
	if err != nil {
		http.Error(w, friendly(err), 400)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (a *App) heads(w http.ResponseWriter, r *http.Request) {
	heads, _ := a.st.ListHeads(r.Context(), false)
	projects, _ := a.st.ListProjects(r.Context(), true)
	a.render(w, r, "heads", PageData{Title: "Heads", Heads: heads, Projects: projects})
}

func (a *App) headSave(w http.ResponseWriter, r *http.Request) {
	if !a.auth.CheckCSRF(r) {
		http.Error(w, "invalid CSRF token", 403)
		return
	}
	id := parseID(r.FormValue("id"))
	projectID := parseID(r.FormValue("project_id"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	hid, err := a.st.UpsertHead(r.Context(), id, projectID, r.FormValue("name"), r.FormValue("due_day"), r.FormValue("active") == "on" || id == 0, sortOrder)
	if err == nil {
		u := auth.CurrentUser(r)
		_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "update", EntityType: "head", EntityID: &hid, Summary: "Saved head " + r.FormValue("name")})
	}
	if err != nil {
		http.Error(w, friendly(err), 400)
		return
	}
	http.Redirect(w, r, "/heads", http.StatusSeeOther)
}

func (a *App) users(w http.ResponseWriter, r *http.Request) {
	users, _ := a.st.ListUsers(r.Context())
	a.render(w, r, "users", PageData{Title: "Users", Users: users})
}

func (a *App) userSave(w http.ResponseWriter, r *http.Request) {
	if !a.auth.CheckCSRF(r) {
		http.Error(w, "invalid CSRF token", 403)
		return
	}
	id := parseID(r.FormValue("id"))
	hash := ""
	if pw := r.FormValue("password"); pw != "" {
		var err error
		hash, err = auth.HashPassword(pw)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	active := r.FormValue("active") == "on" || id == 0
	var err error
	if id == 0 {
		if hash == "" {
			http.Error(w, "password is required", 400)
			return
		}
		_, err = a.st.CreateUser(r.Context(), r.FormValue("email"), r.FormValue("name"), hash, r.FormValue("role"), active)
	} else {
		err = a.st.UpdateUser(r.Context(), id, r.FormValue("name"), r.FormValue("role"), active, hash)
	}
	if err != nil {
		http.Error(w, friendly(err), 400)
		return
	}
	u := auth.CurrentUser(r)
	_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "update", EntityType: "user", Summary: "Saved user " + r.FormValue("email")})
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (a *App) auditLog(w http.ResponseWriter, r *http.Request) {
	entity := strings.TrimSpace(r.URL.Query().Get("entity"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	actor := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("actor")))
	audit, _ := a.st.Audit(r.Context(), entity, parseID(r.URL.Query().Get("id")), 1000)
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
	entries, _ := os.ReadDir(a.cfg.BackupDir)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "backup-") {
			names = append(names, entry.Name())
		}
	}
	a.render(w, r, "backups", PageData{Title: "Backups", Backups: names})
}

func (a *App) backupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := store.Backup(r.Context(), a.cfg.DBPath, a.cfg.AttachmentDir, a.cfg.BackupDir)
	if err != nil {
		a.render(w, r, "backups", PageData{Title: "Backups", Error: err.Error()})
		return
	}
	_ = store.PruneBackups(a.cfg.BackupDir, a.cfg.BackupKeepDays, time.Now())
	u := auth.CurrentUser(r)
	_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "create", EntityType: "backup", Summary: "Created backup " + info.Path})
	http.Redirect(w, r, "/backups", http.StatusSeeOther)
}

func (a *App) report(w http.ResponseWriter, r *http.Request) {
	from := queryDefault(r, "from", queryDefault(r, "month", time.Now().Format("2006-01")))
	to := queryDefault(r, "to", from)
	mode := strings.TrimPrefix(r.URL.Path, "/reports/")
	rows, err := a.st.Report(r.Context(), from, to, mode)
	if err != nil {
		a.render(w, r, "reports", PageData{Title: "Reports", Error: err.Error(), From: from, To: to, Mode: mode})
		return
	}
	a.render(w, r, "reports", PageData{Title: "Reports", Reports: rows, Summary: summarizeReports(rows), From: from, To: to, Mode: mode})
}

func (a *App) exportGrid(w http.ResponseWriter, r *http.Request) {
	month := queryDefault(r, "month", time.Now().Format("2006-01"))
	grid, err := a.st.Grid(r.Context(), month, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u := auth.CurrentUser(r)
	_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported grid " + month})
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="variance-`+month+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Project", "Head", "Budget", "Actual", "Variance", "Variance %", "Status"})
	for _, row := range grid.Rows {
		_ = cw.Write([]string{row.Project, row.Head, money.FormatPaise(row.Budget), money.FormatPaise(row.Actual), money.FormatPaise(row.Variance), row.VariancePercent, row.Status})
	}
	cw.Flush()
}

func (a *App) exportYTD(w http.ResponseWriter, r *http.Request) {
	year := queryDefault(r, "year", time.Now().Format("2006"))
	from, to := year+"-01", year+"-12"
	rows, err := a.st.Report(r.Context(), from, to, "heads")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u := auth.CurrentUser(r)
	_ = a.st.RecordAudit(r.Context(), store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported YTD " + year})
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="ytd-`+year+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Period", "Project", "Head", "Budget", "Actual", "Variance", "Variance %"})
	for _, row := range rows {
		_ = cw.Write([]string{row.Period, row.Project, row.Head, money.FormatPaise(row.Budget), money.FormatPaise(row.Actual), money.FormatPaise(row.Variance), row.VariancePercent})
	}
	cw.Flush()
}

func paymentInput(r *http.Request) (store.PaymentInput, error) {
	amount, err := money.ParsePaise(r.FormValue("amount"))
	if err != nil {
		return store.PaymentInput{}, err
	}
	return store.PaymentInput{
		HeadID:      parseID(r.FormValue("head_id")),
		PaidOn:      r.FormValue("paid_on"),
		Amount:      amount,
		VendorPayee: r.FormValue("vendor_payee"),
		PaymentMode: r.FormValue("payment_mode"),
		InvoiceNo:   r.FormValue("invoice_no"),
		ReferenceNo: r.FormValue("reference_no"),
		Remarks:     r.FormValue("remarks"),
	}, nil
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
	default:
		return err.Error()
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
	default:
		return action
	}
}

func actionClass(action string) string {
	switch action {
	case "void":
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
