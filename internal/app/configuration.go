package app

// The Configuration screen: one form with a fieldset per section, saved into
// app_settings, plus the recoverable categories sub-form. To add a setting,
// add a ConfigField to configSections; the save handler writes only declared
// keys.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// ConfigField is one control. Kind is "text", "number", "toggle" or "select";
// Span is its width in the twelve-column .form-grid.
type ConfigField struct {
	Key, Label, Hint, Kind string
	Min, Max               string
	Options                []ConfigOption
	Span                   int
}

// ConfigOption is one choice of a "select" field.
type ConfigOption struct{ Value, Label string }

// ConfigSection is one <fieldset>: a <legend>, a .form-grid of fields, and an
// optional note underneath for the rule the fields cannot express.
type ConfigSection struct {
	Title  string
	Fields []ConfigField
	Note   string
}

// configSections is the whole screen, in display order. Each field's Key is
// the app_settings key it reads and writes, and the form can write nothing
// outside this table.
var configSections = []ConfigSection{
	{Title: "Request numbering", Fields: []ConfigField{
		{Key: "number_prefix", Label: "Prefix", Kind: "text", Span: 4},
		{Key: "number_year_mode", Label: "Year segment", Kind: "select", Span: 4, Options: []ConfigOption{
			{Value: "calendar", Label: "Calendar year"},
			{Value: "financial", Label: "Financial year"},
			{Value: "none", Label: "None"}}},
		{Key: "number_width", Label: "Number width", Kind: "number", Span: 4, Min: "1", Max: "12",
			Hint: "How many digits the running number is padded to. Whole numbers from 1 to 12."},
	}},
	{Title: "Attachments", Fields: []ConfigField{
		{Key: "require_attachments", Label: "Require a supporting document on every request", Kind: "toggle", Span: 12,
			Hint: "Turning it on does not block submission — it asks for an exception reason when no document is attached, because legitimate documents are sometimes genuinely unavailable."},
		{Key: "attachment_max_mb", Label: "Maximum file size (MiB)", Kind: "number", Span: 6, Hint: "Whole numbers from 1 to 20. One MiB is 1,048,576 bytes."},
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
	// There is deliberately no "allow direct payments" setting: every payment
	// must start from an approved request. An old app_settings row for it may
	// exist and is never read.
	{Title: "Payments", Fields: []ConfigField{
		{Key: "payment_modes", Label: "Payment modes offered", Kind: "text", Span: 12,
			Hint: "Comma separated, in the order the payment form should offer them."},
	}, Note: "Every payment starts from an approved request. There is no direct-entry path, and no setting that opens one."},
	// Read by the reminder scheduler through store.ReminderThresholds. A blank
	// or non-positive value means the default, so reminders cannot be switched
	// off by accident.
	{Title: "Reminders and ageing", Fields: []ConfigField{
		{Key: "reminder_pending_days", Label: "Remind after (days pending)", Kind: "number", Span: 4,
			Hint: "How long a request may sit with an approver before the first reminder. Default 3."},
		{Key: "reminder_repeat_days", Label: "Then repeat every (days)", Kind: "number", Span: 4,
			Hint: "How often the reminder returns while nothing happens. Default 1."},
		{Key: "reminder_stale_days", Label: "Reservation goes stale after (days)", Kind: "number", Span: 4,
			Hint: "How long an accountant may hold a reservation with no payment recorded. Default 1."},
	}, Note: "Reminders are counted in days, not working hours. A request put on hold is waiting on the requester by design and is never reminded about."},
}

// configNumberRules are the whole-number bounds checked on save, in the order
// they are checked, with the message shown when a posted value is outside
// them. Other number fields are not range-checked here.
var configNumberRules = []struct {
	key      string
	min, max int64
	message  string
}{
	{"attachment_max_mb", 1, maxAttachmentMB, "Maximum file size must be a whole number from 1 to 20 MiB."},
	{"number_width", 1, 12, "Number width must be a whole number from 1 to 12."},
}

// configuration shows the Configuration screen.
func (a *App) configuration(w http.ResponseWriter, r *http.Request) {
	a.renderConfiguration(w, r, http.StatusOK, "")
}

// renderConfiguration renders the screen with status and an optional error
// message. The categories are always re-read, so after a refused delete the
// "In use" count shown is the one that refused it.
func (a *App) renderConfiguration(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Recoverable categories are table rows, not app_settings values, so they
	// have their own fieldset and sub-form instead of a ConfigSection.
	catUsage, err := a.st.ListRecoverableCategoriesWithUsage(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "configuration", PageData{
		Title: "Configuration", Config: settings, CategoryUsage: catUsage, Error: errMsg,
	})
}

// recoverableCategorySave creates or updates one recoverable category from
// its own sub-form, so a category can be saved without re-posting the rest of
// the screen. The form's single "requires" select (none, project,
// counterparty or both) is turned into the two stored flags here.
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

// recoverableCategoryDelete deletes a category. If requests still use it the
// store refuses with the count, and the Configuration screen is shown again
// with that message, next to the row's "In use" column and the Active toggle
// the admin can use instead.
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

// configurationSave writes only the keys declared in configSections, so a
// hand-made POST cannot add other settings. Toggles are always written (an
// unchecked box posts nothing, which means off); other fields only when
// posted. A number outside its configNumberRules bounds is refused with 400
// and nothing is saved.
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
	for _, rule := range configNumberRules {
		raw, posted := values[rule.key]
		if !posted {
			continue
		}
		if n, err := strconv.ParseInt(raw, 10, 64); err != nil || n < rule.min || n > rule.max {
			a.renderConfiguration(w, r, http.StatusBadRequest, rule.message)
			return
		}
	}
	if err := a.st.SetAppSettings(r.Context(), auth.CurrentUser(r), values); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}

// boolSetting stores a checkbox value as "1" (on, 1, true, yes) or "0".
func boolSetting(v string) string {
	switch v {
	case "on", "1", "true", "yes":
		return "1"
	default:
		return "0"
	}
}
