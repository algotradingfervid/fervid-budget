package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// The Configuration screen (D6, G21).
//
// One screen, one form, one <fieldset> per section, one generic
// app_settings-backed save. Phase 2 lands the shell plus the Numbering,
// Attachments, Urgency, Approvals and Payments sections; Phase 3 appends its
// reservation fieldset, Phase 4 replaces the standalone recoverable-categories
// page with a fieldset here, and Phase 5 appends Reminders and SMTP.
//
// A later phase must only need to append a ConfigSection. If a phase has to
// touch configuration or configurationSave to add a control, this file got the
// shape wrong.

// ConfigField is one control. Kind is "text", "number", "toggle" or "select";
// Span is its width in the twelve-column .form-grid.
type ConfigField struct {
	Key, Label, Hint, Kind string
	Options                []ConfigOption
	Span                   int
}

type ConfigOption struct{ Value, Label string }

// ConfigSection is one <fieldset>: a <legend>, a .form-grid of fields, and an
// optional note underneath for the rule the fields cannot express.
type ConfigSection struct {
	Title  string
	Fields []ConfigField
	Note   string
}

// configSections is the whole screen. Appending to it is how a later phase adds
// a section; the key of every field is the app_settings key it reads and writes,
// and nothing outside this table can be written by the form.
var configSections = []ConfigSection{
	{Title: "Request numbering", Fields: []ConfigField{
		{Key: "number_prefix", Label: "Prefix", Kind: "text", Span: 4},
		{Key: "number_year_mode", Label: "Year segment", Kind: "select", Span: 4, Options: []ConfigOption{
			{Value: "calendar", Label: "Calendar year"},
			{Value: "financial", Label: "Financial year"},
			{Value: "none", Label: "None"}}},
		{Key: "number_width", Label: "Number width", Kind: "number", Span: 4,
			Hint: "How many digits the running number is padded to."},
	}},
	{Title: "Attachments", Fields: []ConfigField{
		{Key: "require_attachments", Label: "Require a supporting document on every request", Kind: "toggle", Span: 12,
			Hint: "Turning it on does not block submission — it asks for an exception reason when no document is attached, because legitimate documents are sometimes genuinely unavailable."},
		{Key: "attachment_max_mb", Label: "Maximum file size (MB)", Kind: "number", Span: 6},
	}},
	{Title: "Urgency", Fields: []ConfigField{
		{Key: "urgency_mode", Label: "Marking a request urgent", Kind: "select", Span: 6, Options: []ConfigOption{
			{Value: "reason", Label: "Requires a reason"},
			{Value: "free", Label: "Free to mark, no reason"},
			{Value: "disabled", Label: "Disabled"}}},
	}, Note: "Urgent requests follow the same approval rules. Urgency changes who is told and how soon, never who may decide."},
	{Title: "Approvals", Fields: []ConfigField{
		{Key: "allow_approver_choice", Label: "Let the employee choose a different approver", Kind: "toggle", Span: 12,
			Hint: "Off means everyone must use the default approver set on their user record."},
	}, Note: "Self-approval is blocked always. A person can never approve a request they raised, whatever roles they hold."},
	// No "allow direct payments" toggle. The mockup carried one and this table
	// copied it, but nothing ever read the key: paymentCreate refuses a payment
	// with no request whatever the setting says (X5), and the hint promised a
	// written reason and an audit flag that were never built. A control that
	// saves and does nothing, under a hint that is false, is worse than none
	// (recoverables-10). The seeded app_settings row is left in place unread.
	{Title: "Payments", Fields: []ConfigField{
		{Key: "payment_modes", Label: "Payment modes offered", Kind: "text", Span: 12,
			Hint: "Comma separated, in the order the payment form should offer them."},
	}, Note: "Every payment starts from an approved request. There is no direct-entry path, and no setting that opens one."},
	// Phase 5. These were hardcoded 3-day and 1-day waits inside the scheduler;
	// they are data now, read through store.ReminderThresholds. A blank or
	// non-positive value falls back to the default rather than silently
	// disabling reminders, so the form cannot switch them off by accident.
	{Title: "Reminders and ageing", Fields: []ConfigField{
		{Key: "reminder_pending_days", Label: "Remind after (days pending)", Kind: "number", Span: 4,
			Hint: "How long a request may sit with an approver before the first reminder. Default 3."},
		{Key: "reminder_repeat_days", Label: "Then repeat every (days)", Kind: "number", Span: 4,
			Hint: "How often the reminder returns while nothing happens. Default 1."},
		{Key: "reminder_stale_days", Label: "Reservation goes stale after (days)", Kind: "number", Span: 4,
			Hint: "How long an accountant may hold a reservation with no payment recorded. Default 1."},
	}, Note: "Reminders are counted in calendar days, not working hours. A request put on hold is waiting on the requester by design and is never reminded about."},
}

func (a *App) configuration(w http.ResponseWriter, r *http.Request) {
	a.renderConfiguration(w, r, http.StatusOK, "")
}

// renderConfiguration draws the screen, optionally carrying the sentence that
// refused something on it. A refusal re-reads the categories rather than
// re-using whatever the caller was looking at, so the "In use" count printed
// beside a refused delete is the count that refused it.
func (a *App) renderConfiguration(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Recoverable categories are rows, not app_settings scalars, so they cannot
	// be a ConfigSection. They are loaded alongside and rendered by their own
	// fieldset with its own sub-form (D6).
	catUsage, err := a.st.ListRecoverableCategoriesWithUsage(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "configuration", PageData{
		Title: "Configuration", Config: settings, CategoryUsage: catUsage, Error: errMsg,
	})
}

// recoverableCategorySave persists one row of the Configuration screen's
// Recoverable categories fieldset.
//
// It is a sub-form rather than part of the main Save because the main form
// writes scalar app_settings: adding one category must not require re-posting
// every other fieldset. The form carries a single "requires" select rather than
// two raw booleans (D6), and this is the only place that mapping exists.
func (a *App) recoverableCategorySave(w http.ResponseWriter, r *http.Request) {
	var requiresProject, requiresCounterparty bool
	switch r.FormValue("requires") {
	case "project":
		requiresProject = true
	case "counterparty":
		requiresCounterparty = true
	case "both":
		requiresProject, requiresCounterparty = true, true
	case "none", "":
		// nothing extra
	default:
		a.respondError(w, r, http.StatusBadRequest, "That category requirement is not recognised.", nil)
		return
	}
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	_, err := a.st.UpsertRecoverableCategory(r.Context(), auth.CurrentUser(r), parseID(r.FormValue("id")),
		r.FormValue("name"), requiresProject, requiresCounterparty, r.FormValue("active") == "on", sortOrder)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}

// recoverableCategoryDelete is the door F-E-06 reported missing:
// recoverable_category:delete has been grantable from the Roles screen since
// Phase 4 and no route ever consulted it, so an administrator could hand it out
// and buy nothing at all.
//
// The refusal is the interesting half. store.DeleteRecoverableCategory counts
// the requests still pointing at the category inside the same transaction as the
// DELETE and refuses with that count, so this handler must not flatten it into
// "you do not have permission" — the same mistake F-G-023 reported on the roles
// screen, and roleDelete's ErrForbidden branch is the pattern being followed.
//
// It lands back on Configuration rather than on the error page, because the
// count that refused the delete is printed on that screen in the "In use"
// column, immediately beside the row: the reader gets the sentence and the
// evidence for it in one view, and the ordinary remedy — deactivate it instead —
// is the toggle in the next cell.
func (a *App) recoverableCategoryDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteRecoverableCategory(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			a.renderConfiguration(w, r, storeErrorStatus(err), friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}

// configurationSave writes only the keys a registered ConfigField declares, so
// a hand-rolled POST cannot invent a setting the product has never heard of.
// Toggles are written whether or not they were posted, because an unchecked
// checkbox sends nothing at all and "absent" has to mean off.
func (a *App) configurationSave(w http.ResponseWriter, r *http.Request) {
	values := map[string]string{}
	for _, section := range configSections {
		for _, field := range section.Fields {
			if field.Kind == "toggle" {
				values[field.Key] = boolSetting(r.FormValue(field.Key))
				continue
			}
			if _, posted := r.Form[field.Key]; posted {
				values[field.Key] = strings.TrimSpace(r.FormValue(field.Key))
			}
		}
	}
	if err := a.st.SetAppSettings(r.Context(), auth.CurrentUser(r), values); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}

func boolSetting(v string) string {
	switch v {
	case "on", "1", "true", "yes":
		return "1"
	default:
		return "0"
	}
}
