package app

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"fervidbudget/internal/store"
)

// The "Back to report" / "Back to vendor" link on a drill-down page.

// contextReturn returns the back link (href and label) for the current page,
// or two empty strings. It accepts ?report_back= for readers with report:view,
// or a ?return_to= of the form /vendors/{id} for readers with vendor:view;
// anything else is ignored, so the link can never point off-site. The target
// page still checks access itself.
func contextReturn(r *http.Request, perms store.PermissionSet) (string, string) {
	if perms.Can("report", "view") {
		if back := safeReportBack(r.URL.Query().Get("report_back")); back != "" {
			return back, "Back to report"
		}
	}
	if !perms.Can("vendor", "view") {
		return "", ""
	}
	raw := r.URL.Query().Get("return_to")
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Opaque != "" || !strings.HasPrefix(u.Path, "/vendors/") {
		return "", ""
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(u.Path, "/vendors/"), 10, 64)
	if err != nil || id <= 0 {
		return "", ""
	}
	return "/vendors/" + strconv.FormatInt(id, 10), "Back to vendor"
}
