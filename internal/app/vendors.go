package app

import (
	"net/http"

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
