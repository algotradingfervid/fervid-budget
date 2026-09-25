package app

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"fervidbudget/internal/store"
)

// contextReturn keeps a drilldown's visible way back without accepting arbitrary
// redirect destinations. The destination routes still enforce record access.
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
