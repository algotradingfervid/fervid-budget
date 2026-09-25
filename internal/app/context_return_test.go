package app

import (
	"fervidbudget/internal/store"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestContextReturnKeepsOnlyPermittedInternalContext(t *testing.T) {
	all := store.NewPermissionSet([]store.Grant{{Resource: "vendor", Action: "view"}, {Resource: "report", Action: "view"}}, nil)
	cases := []struct{ key, raw, want string }{
		{"return_to", "/vendors/42", "/vendors/42"},
		{"return_to", "//external.example/vendors/42", ""},
		{"return_to", "https://external.example/vendors/42", ""},
		{"return_to", "/vendors/42/../../logout", ""},
		{"return_to", "/vendors/0", ""},
		{"return_to", "/logout", ""},
		{"report_back", "/reports/heads?from=2026-09&to=2026-10&head_id=4", "/reports/heads?from=2026-09&to=2026-10&head_id=4"},
		{"report_back", "//external.example/reports/heads", ""},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/payments/1?"+url.Values{c.key: {c.raw}}.Encode(), nil)
			got, _ := contextReturn(r, all)
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
			if denied, _ := contextReturn(r, store.EmptyPermissions()); denied != "" {
				t.Fatal("offered context without permission", denied)
			}
		})
	}
}
