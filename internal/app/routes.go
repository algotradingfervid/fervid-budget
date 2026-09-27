package app

// The route table: every URL the app serves, the permission it needs and
// the handler behind it.

import (
	"net/http"
)

// routes registers every URL on mux. Most routes use one of four helpers:
//
//   - read: a GET that needs one permission (resource, action).
//   - write: a POST that needs one permission and then goes through withCSRF,
//     which caps and parses the form, checks the CSRF token and, for
//     /requests/{id}/…, answers 404 unless the caller may see that request.
//   - signedIn / signedInForm: the same, for pages that only need a
//     signed-in user because the handler checks the rest itself.
//
// The few routes with a different gate are written out in full. A literal
// path segment beats a {id} wildcard in ServeMux, so /requests/new,
// /vendors/new and /recoverables/list.csv never reach the {id} handlers.
func (a *App) routes(mux *http.ServeMux) {
	read := func(pattern, resource, action string, h http.HandlerFunc) {
		mux.Handle(pattern, a.auth.RequirePermission(resource, action, h))
	}
	write := func(pattern, resource, action string, h http.HandlerFunc) {
		mux.Handle(pattern, a.auth.RequirePermission(resource, action, http.HandlerFunc(a.withCSRF(h))))
	}
	signedIn := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, a.auth.RequireLogin(h))
	}
	signedInForm := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(h))))
	}

	// Public: static files, sign-in and password reset.
	mux.Handle("GET /static/", http.StripPrefix("/static/", publicStatic(a.cfg.DevPages)))
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("GET /login/help", a.loginHelp)
	mux.HandleFunc("POST /login/help", a.withCSRF(a.passwordResetRequest))
	mux.HandleFunc("GET /login/reset", a.passwordResetForm)
	mux.HandleFunc("POST /login/reset", a.withCSRF(a.passwordResetPost))
	// Signing in has no session yet, so there is no CSRF token to check; the
	// body is still capped, and securityBoundary has already refused
	// cross-site posts.
	mux.HandleFunc("POST /login", a.withBodyCap(a.loginPost))
	mux.HandleFunc("POST /logout", a.withCSRF(a.logoutPost))

	// Home is the dashboard. "GET /{$}" matches "/" only; "GET /" catches
	// every other unknown URL and answers 404. The dashboard needs only a
	// session: each work area inside it is shown only to people who may open
	// its queue.
	signedIn("GET /{$}", a.dashboard)
	signedIn("GET /", a.notFound)
	read("GET /grid", "grid", "view", a.grid)
	signedIn("GET /dashboard", a.dashboard)

	// Months and payments.
	read("GET /months", "month", "view", a.months)
	write("POST /months", "month", "create", a.monthCreate)
	read("GET /payments/new", "payment", "create", a.paymentForm)
	write("POST /payments/new", "payment", "create", a.paymentForm)
	read("GET /payments/new/options", "payment", "create", a.paymentPickerOptions)
	read("GET /payments/lock-status", "payment", "create", a.paymentLockStatus)
	read("GET /payments", "payment", "view", a.payments)
	// Recording a payment needs payment:create and payment:settle (the same
	// permission as its preview). Recording a partial payment also needs
	// payment:mark_partial, which paymentCreate checks because it depends on
	// the form.
	mux.Handle("POST /payments", a.auth.RequirePermission("payment", "create",
		a.auth.RequirePermission("payment", "settle", http.HandlerFunc(a.withCSRF(a.paymentCreate)))))
	read("GET /payments/{id}", "payment", "view", a.paymentDetail)
	read("GET /payments/{id}/edit", "payment", "edit", a.paymentEditForm)
	write("POST /payments/{id}/edit", "payment", "edit", a.paymentEdit)
	write("POST /payments/{id}/void", "payment", "void", a.paymentVoid)
	write("POST /payments/{id}/attachments", "attachment", "create", a.attachmentUpload)
	// Payment documents and request documents are separate tables with
	// separate ids, so they have separate routes. Both handlers also check
	// that the caller may see the payment or request the file belongs to.
	read("GET /attachments/{id}", "attachment", "view", a.attachmentDownload)
	read("GET /requests/{id}/attachments/{attachmentID}", "attachment", "view", a.requestAttachmentDownload)

	// Reports and exports.
	read("GET /export.csv", "grid", "export", a.exportGrid)
	read("GET /reports/monthly", "report", "view", a.report)
	read("GET /reports/projects", "report", "view", a.report)
	read("GET /reports/heads", "report", "view", a.report)
	read("GET /reports/ytd.csv", "report", "export", a.exportYTD)

	// Recoverables. Recording money coming back also needs payment:create.
	read("GET /recoverables", "recoverable_report", "view", a.recoverablesDashboard)
	read("GET /recoverables/list", "recoverable_report", "view", a.recoverablesList)
	read("GET /recoverables/list.csv", "recoverable_report", "export", a.exportRecoverable)
	read("GET /recoverables/{id}", "recoverable_report", "view", a.recoverableDetail)
	mux.Handle("POST /recoverables/{id}/events", a.auth.RequirePermission("recoverable_report", "view",
		a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.recordRecovery)))))

	// The user's own notifications: the store only ever returns the caller's
	// own rows, so a session is enough.
	signedIn("GET /notifications", a.notificationCentre)
	signedInForm("POST /notifications/read", a.notificationsMarkAllRead)
	signedIn("GET /notifications/{id}/open", a.notificationOpen)

	// The admin screen for notification rules and email settings.
	read("GET /admin/notifications", "notification", "view", a.adminNotifications)
	write("POST /admin/notifications/smtp", "notification", "edit", a.adminNotificationsSMTP)
	write("POST /admin/notifications/events/{event}", "notification", "edit", a.adminNotificationEventSave)
	write("POST /admin/notifications/test", "notification", "edit", a.adminNotificationsTest)

	// Budgets, month locks and masters. Saving a project or head needs
	// create or edit depending on the posted id, which the handler checks.
	read("GET /budgets/new", "budget", "edit", a.budgetPlannerNew)
	read("GET /budgets/plan", "budget", "edit", a.budgetPlannerEdit)
	read("GET /budgets/plan-data", "budget", "view", a.budgetPlannerData)
	write("POST /budgets/plan", "budget", "edit", a.budgetPlannerSave)
	read("GET /budgets", "budget", "view", a.budgets)
	write("POST /budgets", "budget", "edit", a.budgetSave)
	write("POST /months/{month}/lock", "month", "lock", a.setMonthLock(a.st.LockMonth))
	write("POST /months/{month}/unlock", "month", "lock", a.setMonthLock(a.st.UnlockMonth))
	read("GET /projects", "project", "view", a.projects)
	signedInForm("POST /projects", a.projectSave)
	read("GET /heads", "head", "view", a.heads)
	signedInForm("POST /heads", a.headSave)

	// Vendors.
	read("GET /vendors", "vendor", "view", a.vendorsList)
	read("GET /vendors/new", "vendor", "create", a.vendorNew)
	read("GET /vendors/search", "vendor", "view", a.vendorSearch)
	read("GET /vendors/{id}/{tab}", "vendor", "view", a.vendorRelated)
	read("GET /vendors/{id}", "vendor", "view", a.vendorDetail)
	write("POST /vendors", "vendor", "create", a.vendorCreate)
	write("POST /vendors/{id}", "vendor", "edit", a.vendorUpdate)

	// Payment requests. Creating a request also submits it; there are no
	// drafts.
	read("GET /requests", "request", "view", a.requests)
	read("GET /requests/export.csv", "request", "view", a.requestsExport)
	read("GET /requests/new", "request", "create", a.requestNew)
	read("GET /requests/new/fields", "request", "create", a.requestFormFields)
	write("POST /requests/duplicate-check", "request", "create", a.requestDuplicateCheck)
	write("POST /requests", "request", "create", a.requestCreate)
	read("GET /requests/{id}/submitted", "request", "view", a.requestSubmitted)

	// Paying a request: reserving it, the settlement preview (a POST only
	// because it carries the form; it saves nothing), and the reservation.
	write("POST /requests/{id}/record-payment", "reservation", "reserve", a.requestRecordPayment)
	write("POST /requests/{id}/settlement-preview", "payment", "settle", a.settlementPreview)
	// The reservation page serves both the holder releasing it and an admin
	// taking it over, which are different permissions, so the page needs only a
	// session and reservationForm checks the one that fits the caller. Each POST
	// has its own gate.
	signedIn("GET /requests/{id}/reservation", a.reservationForm)
	write("POST /requests/{id}/release", "reservation", "release", a.requestRelease)
	write("POST /requests/{id}/reassign", "reservation", "reassign", a.requestReassign)
	// The "reserved a long time ago" prompt; each choice on it is gated again.
	read("GET /requests/{id}/reservation/stale", "payment", "process", a.requestStale)
	// One permission, payment:hold, both puts a request on hold and takes it off.
	write("POST /requests/{id}/hold", "payment", "hold", a.requestHold)
	write("POST /requests/{id}/unhold", "payment", "hold", a.requestUnhold)
	read("GET /accounts-queue", "payment", "process", a.accountsQueue)
	// A partial payment goes back to the approver. Anyone who can see the
	// request may read the review page; accepting it or raising a concern
	// needs approval:accept_partial.
	read("GET /requests/{id}/partial-review", "request", "view", a.requestPartialReview)
	write("POST /requests/{id}/accept-partial", "approval", "accept_partial", a.requestAcceptPartial)
	write("POST /requests/{id}/raise-concern", "approval", "accept_partial", a.requestRaiseConcern)

	// The request detail page is the same for everyone; each decision posts
	// to its own route with the permission that decision needs.
	read("GET /requests/{id}", "request", "view", a.requestDetail)
	read("GET /requests/{id}/edit", "request", "edit", a.requestEditForm)
	write("POST /requests/{id}/edit", "request", "edit", a.requestEdit)
	write("POST /requests/{id}/comment", "request", "comment", a.requestComment)
	// Adding a document works without edit rights, so a requester can still
	// send the document Accounts asked for while the request is on hold.
	write("POST /requests/{id}/attachments", "attachment", "create", a.requestAttachmentUpload)
	write("POST /requests/{id}/withdraw", "request", "withdraw", a.requestWithdraw)
	write("POST /requests/{id}/reraise", "request", "reraise", a.requestReraise)
	write("POST /requests/{id}/approve", "approval", "approve", a.requestApprove)
	write("POST /requests/{id}/return", "approval", "return", a.requestReturn)
	write("POST /requests/{id}/reject", "approval", "reject", a.requestReject)
	// Handing the approval to another approver (not the same as
	// /reassign, which moves a payment reservation).
	write("POST /requests/{id}/reassign-approver", "approval", "reassign", a.requestReassignApprover)
	// Cancellation: the requester asks (GET form, POST cancel-request); an
	// approver cancels outright (POST cancel) or decides on a request to
	// cancel (cancellation).
	read("GET /requests/{id}/cancel", "request", "cancel", a.requestCancelForm)
	write("POST /requests/{id}/cancel-request", "request", "cancel", a.requestCancelAsk)
	write("POST /requests/{id}/cancel", "approval", "cancel", a.requestCancelOutright)
	read("GET /requests/{id}/cancellation", "approval", "cancel", a.requestCancellationForm)
	write("POST /requests/{id}/cancellation", "approval", "cancel", a.requestCancellationDecide)
	read("GET /approvals", "approval", "approve", a.approvals)

	// Administration.
	read("GET /users", "user", "view", a.users)
	// Adding a user needs user:create and editing one needs user:edit; which
	// applies depends on the posted id. The route refuses anyone with
	// neither, before the form is read, and userSave checks the exact one.
	mux.Handle("POST /users", a.requireAnyOf("user", []string{"create", "edit"}, http.HandlerFunc(a.withCSRF(a.userSave))))
	read("GET /roles", "role", "view", a.rolesPage)
	write("POST /roles", "role", "edit", a.rolesSave)
	write("POST /roles/new", "role", "create", a.roleCreate)
	write("POST /roles/{id}/copy", "role", "create", a.roleCopy)
	write("POST /roles/{id}/delete", "role", "delete", a.roleDelete)
	read("GET /configuration", "config", "view", a.configuration)
	write("POST /configuration", "config", "edit", a.configurationSave)
	// Recoverable categories live on the configuration screen but have
	// their own permissions; deleting one is separate from editing it.
	write("POST /configuration/recoverable-categories", "recoverable_category", "edit", a.recoverableCategorySave)
	write("POST /configuration/recoverable-categories/{id}/delete", "recoverable_category", "delete", a.recoverableCategoryDelete)
	read("GET /audit", "audit", "view", a.auditLog)
	read("GET /backups", "backup", "view", a.backups)
	write("POST /backups", "backup", "create", a.backupCreate)
}
