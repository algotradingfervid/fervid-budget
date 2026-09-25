package app

import (
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// The vendor master's screens (adoption-spec D3 / gap G5).
//
// Every read here hands the caller's PermissionSet to the store, which is what
// decides whether the bank block comes back at all. The templates gate on
// .Perms too, but that is presentation: by the time a template runs, a caller
// without vendor_bank:view is holding a Vendor whose Bank is nil, so there is
// nothing for a mistaken {{...}} to print.

func (a *App) vendorsList(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	query := r.URL.Query()
	opt := store.VendorListOptions{
		Query:        query.Get("q"),
		VendorType:   query.Get("type"),
		Category:     query.Get("category"),
		Status:       queryDefault(r, "status", "active"),
		MissingGSTIN: query.Get("gap") == "gstin",
	}
	vendors, err := a.st.ListVendors(r.Context(), opt, perms)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	stats, err := a.st.VendorStats(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	categories, err := a.st.VendorCategories(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var paid int64
	open := 0
	for _, v := range vendors {
		paid += v.PaidThisYear
		open += v.OpenRequests
	}
	a.render(w, r, "vendors_list", PageData{
		Title:            "Vendors",
		Vendors:          vendors,
		VendorStats:      stats,
		VendorCategories: categories,
		VendorPaidTotal:  paid,
		VendorOpenTotal:  open,
		Query:            opt.Query,
		Status:           opt.Status,
		VendorType:       opt.VendorType,
		VendorCategory:   opt.Category,
		VendorGap:        query.Get("gap"),
	})
}

// vendorNew is the blank form. The empty bank block is handed to a caller who
// holds vendor_bank:view because "this vendor has no bank details yet" is
// something they are entitled to be told; a caller without the permission gets
// nil and the locked banner, exactly as on an existing vendor.
func (a *App) vendorNew(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	v := store.Vendor{VendorType: "company", Status: "active"}
	if perms.Can("vendor_bank", "view") {
		v.Bank = &store.VendorBank{}
	}
	a.renderVendorForm(w, r, http.StatusOK, v, true, "")
}

// vendorSearch is the request form's combobox, served as HTML rather than
// JSON: htmx swaps the markup straight into .combo-list, so there is no client
// renderer to keep in step with the server's idea of a vendor. It renders no
// shell — Phase 0's HX-Request handling skips shell construction, and this
// template starts at .combo-list rather than at "top".
func (a *App) vendorSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	vendors, err := a.st.SearchVendors(r.Context(), q, 0)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "vendor_combo_options", PageData{Title: "Vendors", Vendors: vendors, Query: q})
}

func (a *App) vendorDetail(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	v, err := a.st.Vendor(r.Context(), pathID(r), perms)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderVendorForm(w, r, http.StatusOK, v, perms.Can("vendor", "edit"), "")
}

func (a *App) vendorCreate(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	in := vendorInputFromForm(r, perms)
	id, err := a.st.CreateVendor(r.Context(), auth.CurrentUser(r), in)
	if err != nil {
		if status := storeErrorStatus(err); status < http.StatusInternalServerError {
			a.renderVendorForm(w, r, status, vendorFromInput(0, in), true, friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/vendors/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) vendorUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	perms := a.auth.Permissions(auth.CurrentUser(r))
	in := vendorInputFromForm(r, perms)
	if err := a.st.UpdateVendor(r.Context(), auth.CurrentUser(r), id, in, perms); err != nil {
		if status := storeErrorStatus(err); status < http.StatusInternalServerError && status != http.StatusNotFound {
			a.renderVendorForm(w, r, status, vendorFromInput(id, in), true, friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/vendors/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) renderVendorForm(w http.ResponseWriter, r *http.Request, status int, v store.Vendor, editable bool, errMsg string) {
	title := "Add vendor"
	if v.ID != 0 {
		title = v.Name
	}
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	mode := ""
	if v.Bank != nil {
		mode = v.Bank.DefaultPaymentMode
	}
	gstError := ""
	if strings.Contains(strings.ToLower(errMsg), "gstin") {
		gstError = errMsg
	}
	a.renderStatus(w, r, status, "vendor_detail", PageData{
		VendorModeOptions: vendorPaymentModes(settings, mode), VendorGSTError: gstError,
		Title:          title,
		Vendor:         v,
		VendorEditable: editable,
		Error:          errMsg,
	})
}

// vendorInputFromForm reads the submitted record. The bank block is read at
// all only for a caller holding vendor_bank:edit — CreateVendor takes no
// permission set, so this is where a create is gated. UpdateVendor applies the
// same rule again on its own; neither relies on the other.
func vendorInputFromForm(r *http.Request, perms store.PermissionSet) store.VendorInput {
	in := store.VendorInput{
		Name:          r.FormValue("name"),
		DisplayName:   r.FormValue("display_name"),
		VendorType:    r.FormValue("vendor_type"),
		Status:        r.FormValue("status"),
		Categories:    r.FormValue("categories"),
		GSTIN:         r.FormValue("gstin"),
		PAN:           r.FormValue("pan"),
		MSMEUdyam:     r.FormValue("msme_udyam"),
		TDSSection:    r.FormValue("tds_section"),
		TDSRate:       r.FormValue("tds_rate"),
		ContactPerson: r.FormValue("contact_person"),
		Phone:         r.FormValue("phone"),
		Email:         r.FormValue("email"),
		Address:       r.FormValue("address"),
		City:          r.FormValue("city"),
		State:         r.FormValue("state"),
		StateCode:     r.FormValue("state_code"),
		Notes:         r.FormValue("notes"),
	}
	if perms != nil && perms.Can("vendor_bank", "edit") {
		terms, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("payment_terms_days")))
		in.Bank = &store.VendorBank{
			AccountName:        r.FormValue("bank_account_name"),
			AccountNumber:      r.FormValue("bank_account_number"),
			IFSC:               r.FormValue("bank_ifsc"),
			BankName:           r.FormValue("bank_name"),
			Branch:             r.FormValue("bank_branch"),
			UPIID:              r.FormValue("upi_id"),
			DefaultPaymentMode: r.FormValue("default_payment_mode"),
			PaymentTermsDays:   terms,
		}
	}
	return in
}

// vendorFromInput re-renders a rejected submission without losing what was
// typed. Bank rides along only when the submitter was allowed to send it, so a
// failed save cannot echo a block back to someone who may not see it.
func vendorFromInput(id int64, in store.VendorInput) store.Vendor {
	return store.Vendor{
		ID: id, Name: in.Name, DisplayName: in.DisplayName,
		VendorType: in.VendorType, Status: in.Status, Categories: in.Categories,
		GSTIN: in.GSTIN, PAN: in.PAN, MSMEUdyam: in.MSMEUdyam,
		TDSSection: in.TDSSection, TDSRate: in.TDSRate,
		ContactPerson: in.ContactPerson, Phone: in.Phone, Email: in.Email,
		Address: in.Address, City: in.City, State: in.State, StateCode: in.StateCode,
		Notes: in.Notes, Bank: in.Bank,
	}
}

// vendorRelated keeps the supplier context while applying the reader's ordinary
// request/payment scope. Vendor access alone never grants access to its records.
func (a *App) vendorRelated(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	perms := a.auth.Permissions(u)
	v, err := a.st.Vendor(r.Context(), pathID(r), perms)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	tab := r.PathValue("tab")
	resource := map[string]string{"requests": "request", "payments": "payment", "history": "audit"}[tab]
	if resource == "" {
		http.NotFound(w, r)
		return
	}
	if !perms.Can(resource, "view") {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to view these records.", nil)
		return
	}
	offset := int(parseID(r.URL.Query().Get("offset")))
	if offset < 0 {
		offset = 0
	}
	data := PageData{Title: v.Name, Vendor: v, VendorTab: tab, Page: store.RequestPage{Offset: offset, Limit: 50}}
	switch tab {
	case "requests":
		page, e := a.st.ListRequestsPage(r.Context(), store.RequestPageOptions{RequestListOptions: store.RequestListOptions{VendorID: v.ID, Scope: a.effectiveScope(u, "all"), ViewerID: u.ID, Bucket: "all", Limit: 50}, Offset: offset})
		err = e
		data.Requests, data.Page = page.Requests, page
	case "payments":
		data.Payments, err = a.st.ListPayments(r.Context(), store.PaymentListOptions{VendorID: v.ID, Scope: perms.Scope("payment"), ViewerID: u.ID, Status: "all", Limit: 51, Offset: offset})
		if len(data.Payments) > 50 {
			data.Page.Truncated = true
			data.Payments = data.Payments[:50]
		}
	case "history":
		page, e := a.st.AuditPage(r.Context(), store.AuditQuery{EntityType: "vendor", EntityID: v.ID, Limit: 50, Offset: offset})
		err = e
		data.Audit = page.Entries
		data.Page.Truncated = page.Truncated
		data.Page.Total = page.Total
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "vendor_related", data)
}

func vendorPaymentModes(settings map[string]string, current string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range strings.Split(settings["payment_modes"], ",") {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	// Preserve an older/custom value on unrelated edits, even if configuration
	// no longer offers it for new vendors.
	if current != "" && !seen[current] {
		out = append(out, current)
	}
	return out
}

type PaymentModeChoice struct{ Value, Label string }

func canonicalPaymentMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "neft", "bank transfer", "bank_transfer":
		return "bank_transfer"
	case "rtgs":
		return "rtgs"
	case "dd", "demand draft":
		return "dd"
	case "upi":
		return "upi"
	case "cheque":
		return "cheque"
	case "cash":
		return "cash"
	case "card":
		return "card"
	case "other":
		return "other"
	default:
		return strings.TrimSpace(value)
	}
}
func configuredPaymentChoices(settings map[string]string) []PaymentModeChoice {
	out := []PaymentModeChoice{}
	seen := map[string]bool{}
	for _, label := range strings.Split(settings["payment_modes"], ",") {
		label = strings.TrimSpace(label)
		value := canonicalPaymentMode(label)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, PaymentModeChoice{Value: value, Label: label})
		}
	}
	return out
}
