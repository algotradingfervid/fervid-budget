package app

// The route table: every URL the app serves, the permission it needs and
// the handler behind it.

import (
	"net/http"
)

func (a *App) routes(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", publicStatic(a.cfg.DevPages)))
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("GET /login/help", a.loginHelp)
	mux.HandleFunc("POST /login/help", a.withCSRF(a.passwordResetRequest))
	mux.HandleFunc("GET /login/reset", a.passwordResetForm)
	mux.HandleFunc("POST /login/reset", a.withCSRF(a.passwordResetPost))
	// Login has no prior session; browser origin checks run before parsing.
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
	mux.Handle("POST /payments/new", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.paymentForm))))
	mux.Handle("GET /payments/new/options", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentPickerOptions)))
	mux.Handle("GET /payments/lock-status", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentLockStatus)))
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
	mux.Handle("POST /recoverables/{id}/events", a.auth.RequirePermission("recoverable_report", "view", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.recordRecovery)))))

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
	mux.Handle("GET /budgets/new", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.budgetPlannerNew)))
	mux.Handle("GET /budgets/plan", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.budgetPlannerEdit)))
	mux.Handle("GET /budgets/plan-data", a.auth.RequirePermission("budget", "view", http.HandlerFunc(a.budgetPlannerData)))
	mux.Handle("POST /budgets/plan", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.withCSRF(a.budgetPlannerSave))))
	mux.Handle("GET /budgets", a.auth.RequirePermission("budget", "view", http.HandlerFunc(a.budgets)))
	mux.Handle("POST /budgets", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.withCSRF(a.budgetSave))))
	mux.Handle("POST /months/{month}/lock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.setMonthLock(a.st.LockMonth)))))
	mux.Handle("POST /months/{month}/unlock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.setMonthLock(a.st.UnlockMonth)))))
	mux.Handle("GET /projects", a.auth.RequirePermission("project", "view", http.HandlerFunc(a.projects)))
	mux.Handle("POST /projects", a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(a.projectSave))))
	mux.Handle("GET /heads", a.auth.RequirePermission("head", "view", http.HandlerFunc(a.heads)))
	mux.Handle("POST /heads", a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(a.headSave))))
	// Literal segments beat the {id} wildcard in ServeMux's specificity rules,
	// so /vendors/new is the form and never a vendor whose id parses to zero.
	mux.Handle("GET /vendors", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorsList)))
	mux.Handle("GET /vendors/new", a.auth.RequirePermission("vendor", "create", http.HandlerFunc(a.vendorNew)))
	mux.Handle("GET /vendors/search", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorSearch)))
	mux.Handle("GET /vendors/{id}/{tab}", a.auth.RequirePermission("vendor", "view", http.HandlerFunc(a.vendorRelated)))
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
	// Q6/L7: a held request still accepts the requester's documents without the
	// edit path, which is closed once a request is approved. The Phase-2 route
	// table planned this door (attachment:create) and it was never built, so the
	// document Accounts asked for had no way in (hold-1).
	mux.Handle("POST /requests/{id}/attachments", a.auth.RequirePermission("attachment", "create", http.HandlerFunc(a.withCSRF(a.requestAttachmentUpload))))
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
	// Delete is its own verb and its own route: edit may retire a category by
	// unticking Active, and removing the row outright is a different power over
	// the same table (F-E-06). Registered after the collection POST so the
	// literal path keeps its own handler; ServeMux prefers the more specific
	// pattern either way.
	mux.Handle("POST /configuration/recoverable-categories/{id}/delete", a.auth.RequirePermission("recoverable_category", "delete", http.HandlerFunc(a.withCSRF(a.recoverableCategoryDelete))))
	mux.Handle("GET /audit", a.auth.RequirePermission("audit", "view", http.HandlerFunc(a.auditLog)))
	mux.Handle("GET /backups", a.auth.RequirePermission("backup", "view", http.HandlerFunc(a.backups)))
	mux.Handle("POST /backups", a.auth.RequirePermission("backup", "create", http.HandlerFunc(a.withCSRF(a.backupCreate))))
}
