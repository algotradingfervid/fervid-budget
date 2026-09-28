package app

import (
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// Vendor screens: the vendor list, the add/edit form, the search box used by
// the request form, and a vendor's related requests, payments and history.
//
// Every read passes the reader's permissions to the store, which leaves out
// the bank details (Vendor.Bank is nil) unless they hold vendor_bank:view. The
// templates also check permissions, but the data is already absent.

// vendorsList shows the vendor list with its filters, stats and the totals of
// paid-this-year and open requests over the listed vendors.
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

// vendorNew shows the blank vendor form. A reader with vendor_bank:view gets
// an empty bank section; one without it sees the "locked" banner instead, as
// on an existing vendor.
func (a *App) vendorNew(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	v := store.Vendor{VendorType: "company", Status: "active"}
	if perms.Can("vendor_bank", "view") {
		v.Bank = &store.VendorBank{}
	}
	a.renderVendorForm(w, r, http.StatusOK, v, true, "")
}

// vendorSearch returns the vendor options for the request form's search box as
// an HTML fragment, which htmx puts straight into the list (no page shell).
func (a *App) vendorSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	vendors, err := a.st.SearchVendors(r.Context(), q, 0)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "vendor_combo_options", PageData{Title: "Vendors", Vendors: vendors, Query: q})
}

// vendorDetail shows one vendor's form, editable for readers with vendor:edit.
func (a *App) vendorDetail(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	v, err := a.st.Vendor(r.Context(), pathID(r), perms)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderVendorForm(w, r, http.StatusOK, v, perms.Can("vendor", "edit"), "")
}

// vendorCreate saves a new vendor and opens it. A refusal the user can fix
// shows the form again with the message and what was typed.
func (a *App) vendorCreate(w http.ResponseWriter, r *http.Request) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	in := vendorInputFromForm(r, perms)
	id, err := a.st.CreateVendor(r.Context(), auth.CurrentUser(r), in)
	if err != nil {
		if status, ok := userError(err); ok {
			a.renderVendorForm(w, r, status, vendorFromInput(0, in), true, friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/vendors/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// vendorUpdate saves changes to a vendor, like vendorCreate. The store applies
// the bank-details permission again on its own.
func (a *App) vendorUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	perms := a.auth.Permissions(auth.CurrentUser(r))
	in := vendorInputFromForm(r, perms)
	if err := a.st.UpdateVendor(r.Context(), auth.CurrentUser(r), id, in, perms); err != nil {
		// A vendor that no longer exists gets the not-found page, not the form.
		if status, ok := userError(err); ok && status != http.StatusNotFound {
			a.renderVendorForm(w, r, status, vendorFromInput(id, in), true, friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/vendors/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// renderVendorForm draws the vendor form with status. The payment-mode choices
// come from settings (keeping the vendor's current mode), and an error message
// mentioning GSTIN is also shown next to the GSTIN field.
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

// vendorInputFromForm reads the posted vendor. The bank details are read only
// when the reader holds vendor_bank:edit; this is the only check for a create,
// since CreateVendor takes no permissions. UpdateVendor checks again itself.
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
	// Bank fields are read only from someone who can see and edit them; the form
	// shows no bank fields to anyone else, so theirs would arrive blank.
	if perms != nil && perms.Can("vendor_bank", "view") && perms.Can("vendor_bank", "edit") {
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

// vendorFromInput turns a rejected submission back into a Vendor so the form
// shows what was typed. Bank is set only if the submitter was allowed to send
// it, so a failed save never shows bank details to someone who may not see them.
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

// vendorRelated shows one tab of a vendor's related records: requests,
// payments or history, 50 per page. Each tab needs the matching view
// permission and applies the reader's normal data scope; access to the vendor
// does not grant access to its records.
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
