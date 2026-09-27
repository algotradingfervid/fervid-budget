package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestConfigurationNumberWidthRejectsValuesTheNumberGeneratorWouldIgnore(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, raw := range []string{"0", "-1", "13", "1.5", "invalid", ""} {
		resp := s.postForm("/configuration", url.Values{"number_width": {raw}, "number_prefix": {"Must not save"}})
		requireStatus(t, resp, http.StatusBadRequest)
		if body := responseBody(t, resp); !strings.Contains(body, "Number width must be a whole number from 1 to 12.") {
			t.Fatalf("missing actionable validation for %q", raw)
		}
		width, err := s.st.AppSetting(s.ctx, "number_width")
		if err != nil || width != "6" {
			t.Fatalf("invalid width persisted: %q %v", width, err)
		}
		prefix, err := s.st.AppSetting(s.ctx, "number_prefix")
		if err != nil || prefix == "Must not save" {
			t.Fatalf("rejected form partially saved: %q %v", prefix, err)
		}
	}
	for _, width := range []string{"1", "12"} {
		resp := s.postForm("/configuration", url.Values{"number_width": {width}})
		requireStatus(t, resp, http.StatusSeeOther)
		_ = responseBody(t, resp)
		got, err := s.st.AppSetting(s.ctx, "number_width")
		if err != nil || got != width {
			t.Fatalf("valid boundary failed: %q %v", got, err)
		}
	}
}
