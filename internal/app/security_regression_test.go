package app

import (
	"bytes"
	"context"
	"html/template"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecurityBoundaryOriginsLimitsAndHeaders(t *testing.T) {
	a := &App{}
	handler := a.securityBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		origin, site string
		want         int
	}{{"http://example.test", "same-origin", 204}, {"http://attacker.test", "cross-site", 403}, {"http://sibling.example.test", "same-site", 403}, {"null", "cross-site", 403}} {
		r := httptest.NewRequest("POST", "http://example.test/login", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("origin %s: %d", tc.origin, w.Code)
		}
		for _, key := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control"} {
			if w.Header().Get(key) == "" {
				t.Errorf("missing %s", key)
			}
		}
	}
	for i := 0; i < 31; i++ {
		r := httptest.NewRequest("POST", "http://example.test/login", nil)
		r.RemoteAddr = "192.0.2.20:4567"
		r.Header.Set("X-Forwarded-For", "1.2.3.4")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i == 30 && w.Code != 429 {
			t.Fatalf("login source limit: %d", w.Code)
		}
	}
	if got := requestBodyLimit(httptest.NewRequest("POST", "/login", nil)); got != 8<<10 {
		t.Fatalf("login cap: %d", got)
	}
	for _, p := range []string{"/", "/_kitchensink.html", "/unexpected-secret.env"} {
		w := httptest.NewRecorder()
		publicStatic().ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("static path %s: %d", p, w.Code)
		}
	}
}

func TestSecurityLogoutRevokesCopiedCookieAndBoundsAudit(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	u, _ := url.Parse(s.server.URL)
	cookies := s.client.Jar.Cookies(u)
	responseBody(t, s.postForm("/logout", nil))
	r, _ := http.NewRequest("GET", s.server.URL+"/users", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("copied cookie remains valid: %d", resp.StatusCode)
	}
	form := url.Values{"email": {strings.Repeat("x", 4000)}, "password": {"wrong"}}
	responseBody(t, s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
	var largest int
	if err = s.st.DB().QueryRow(`SELECT COALESCE(MAX(length(actor_name)),0) FROM audit_log WHERE action='login_failed'`).Scan(&largest); err != nil {
		t.Fatal(err)
	}
	if largest > 254 {
		t.Fatalf("unbounded audit email: %d", largest)
	}
}

func TestSecurityCSVAndAttachmentValidationQuotaAndContainment(t *testing.T) {
	for _, value := range []string{"=SUM(1,2)", " +cmd", "\t@bad", "\rformula", "\uFEFF=bad", "-formula"} {
		if !strings.HasPrefix(csvText(value), "'") {
			t.Fatalf("unsafe CSV: %q", value)
		}
	}
	if csvText("Ordinary vendor") != "Ordinary vendor" {
		t.Fatal("ordinary CSV changed")
	}
	for _, data := range [][]byte{[]byte("%PDF-1.4\nnot a document\n%%EOF"), append([]byte(validTestPDF), []byte("<script>bad()</script>")...)} {
		if _, err := validatedAttachmentType(bytes.NewReader(data), "file.pdf"); err == nil {
			t.Fatal("invalid PDF accepted")
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	payload := append(append([]byte{}, encoded.Bytes()...), []byte("APPENDED-PAYLOAD")...)
	var clean bytes.Buffer
	if _, err := copyAttachment(&clean, bytes.NewReader(payload), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(clean.Bytes(), []byte("APPENDED-PAYLOAD")) {
		t.Fatal("image payload survived re-encoding")
	}
	s := newAppTestServer(t)
	a := s.probeApp()
	a.tpl = template.Must(template.New("error_page").Parse(`{{.Error}}`))
	a.cfg.AttachmentQuotaBytes = 1
	body, ct := attachmentMultipart(t, nil, "valid.png", encoded.Bytes())
	r := httptest.NewRequest("POST", "/requests", body)
	r.Header.Set("Content-Type", ct)
	if _, _, err := a.stageUploadedAttachment(r); err == nil {
		t.Fatal("quota bypass")
	}
	if r.MultipartForm != nil {
		r.MultipartForm.RemoveAll()
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("SECRET-OUTSIDE"), 0600)
	link := filepath.Join(a.cfg.AttachmentDir, "escaped")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.serveAttachmentFile(w, httptest.NewRequest("GET", "/attachments/1", nil), 1, link, "file.txt", "text/plain")
	if w.Code != 404 || strings.Contains(w.Body.String(), "SECRET-OUTSIDE") {
		t.Fatalf("symlink escaped: %d", w.Code)
	}
}

func TestSecurityInvalidResetTokenHasNoSideEffects(t *testing.T) {
	s := newAppTestServer(t)
	responseBody(t, s.request("GET", "/login/help", nil, ""))
	before, _ := s.st.UserByEmail(context.Background(), s.cfg.AdminEmail)
	resp := s.postForm("/login/reset", url.Values{"token": {strings.Repeat("a", 64)}, "password": {"NewPassword12345"}, "confirm_password": {"NewPassword12345"}})
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	after, _ := s.st.UserByEmail(context.Background(), s.cfg.AdminEmail)
	if before.PasswordHash != after.PasswordHash || before.SessionVersion != after.SessionVersion {
		t.Fatal("invalid reset changed account")
	}
	// Limiter state expires and cannot grow indefinitely.
	var l authLimiter
	now := time.Now()
	for i := 0; i < 30; i++ {
		if !l.allow("ip", 30, now) {
			t.Fatal("premature limit")
		}
	}
	if l.allow("ip", 30, now) {
		t.Fatal("limit bypass")
	}
	if !l.allow("ip", 30, now.Add(time.Minute)) {
		t.Fatal("limit failed to expire")
	}
}
