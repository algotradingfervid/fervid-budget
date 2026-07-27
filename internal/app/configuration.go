package app

import (
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
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
	{Title: "Payments", Fields: []ConfigField{
		{Key: "allow_direct_payments", Label: "Allow direct payments without a request", Kind: "toggle", Span: 12,
			Hint: "Off. Every new payment starts from an approved request. Turning this on demands a written reason and is flagged in the audit log."},
		{Key: "payment_modes", Label: "Payment modes offered", Kind: "text", Span: 12,
			Hint: "Comma separated, in the order the payment form should offer them."},
	}},
}

func (a *App) configuration(w http.ResponseWriter, r *http.Request) {
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
	a.render(w, r, "configuration", PageData{Title: "Configuration", Config: settings, CategoryUsage: catUsage})
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
