package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

const testAdminPassword = "AdminPass1234"

type appTestServer struct {
	t      *testing.T
	ctx    context.Context
	cfg    config.Config
	st     *store.Store
	http   *http.Server
	server *httptest.Server
	client *http.Client
}

func newAppTestServer(t *testing.T) *appTestServer {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		DBPath:           filepath.Join(dir, "fervid.db"),
		AttachmentDir:    filepath.Join(dir, "attachments"),
		BackupDir:        filepath.Join(dir, "backups"),
		SessionKey:       "test-session-key-that-is-long-enough",
		AdminEmail:       "admin@example.test",
		AdminName:        "Test Admin",
		AdminPassword:    testAdminPassword,
		BackupKeepDays:   1,
		SMTPAllowedHosts: []string{"smtp.example.test"},
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	httpServer, err := New(cfg, st)
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpServer.Handler)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() {
		server.Close()
		_ = st.Close()
	})
	return &appTestServer{t: t, ctx: context.Background(), cfg: cfg, st: st, http: httpServer, server: server, client: client}
}

func (s *appTestServer) request(method, path string, body io.Reader, contentType string) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(method, s.server.URL+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

func responseBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := responseBody(t, resp)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
}

func (s *appTestServer) login(email, password string) {
	s.t.Helper()
	form := url.Values{"email": {email}, "password": {password}}
	resp := s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		s.t.Fatalf("login status = %d, want 303; body: %s", resp.StatusCode, body)
	}
}

// loginAdmin signs in as the bootstrap administrator.
func (s *appTestServer) loginAdmin() {
	s.t.Helper()
	s.login(s.cfg.AdminEmail, testAdminPassword)
}

// get fetches a page and returns its body, whatever the status.
func (s *appTestServer) get(path string) string {
	s.t.Helper()
	return responseBody(s.t, s.request(http.MethodGet, path, nil, ""))
}

// csrf is the session's CSRF token, which every form post must carry.
func (s *appTestServer) csrf() string {
	s.t.Helper()
	u, _ := url.Parse(s.server.URL)
	for _, c := range s.client.Jar.Cookies(u) {
		if c.Name == "fervid_csrf" {
			return c.Value
		}
	}
	s.t.Fatal("CSRF cookie was not set")
	return ""
}

func (s *appTestServer) postForm(path string, form url.Values) *http.Response {
	s.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	// Existing form helpers represent a freshly opened correction screen. Clone
	// first so repeated calls do not accidentally retain a prior screen revision.
	copyForm := url.Values{}
	for key, values := range form {
		copyForm[key] = append([]string(nil), values...)
	}
	form = copyForm
	if path == "/payments" && form.Get("request_id") != "" && len(form.Get("paid_on")) == 10 {
		if err := backdateApproval(s.ctx, s.st.DB(), form.Get("request_id"), form.Get("paid_on")); err != nil {
			s.t.Fatal(err)
		}
	}

	if strings.HasPrefix(path, "/requests/") && (strings.HasSuffix(path, "/edit") || strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/return") || strings.HasSuffix(path, "/reject") || strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/cancellation")) && !form.Has("revision") {
		pagePath := path
		if !strings.HasSuffix(path, "/edit") {
			pagePath = path[:strings.LastIndex(path, "/")]
		}
		// Both cancellation decisions are posted from the approver's
		// cancellation screen.
		if strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/cancellation") {
			pagePath += "/cancellation"
		}
		page := responseBody(s.t, s.request(http.MethodGet, pagePath, nil, ""))
		matches := regexp.MustCompile(`name="revision" value="([^"]+)"`).FindStringSubmatch(page)
		if len(matches) == 2 {
			form.Set("revision", matches[1])
		}
	}
	form.Set("csrf", s.csrf())
	return s.request(http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

// postFormHX is postForm with the header htmx sets on every request it makes,
// so a fragment endpoint is exercised the way the browser will reach it.
func (s *appTestServer) postFormHX(path string, form url.Values) *http.Response {
	s.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", s.csrf())
	req, err := http.NewRequest(http.MethodPost, s.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

// htmxGet fetches a page with the header htmx sets, so the server answers with
// the fragment rather than the whole page.
func (s *appTestServer) htmxGet(path string) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.server.URL+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

// attachmentMultipart builds a multipart form carrying the given fields and one
// file in the "attachment" field, and returns it with its content type.
func attachmentMultipart(t *testing.T, fields url.Values, name string, contents []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, values := range fields {
		for _, value := range values {
			if err := mw.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := mw.CreateFormFile("attachment", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err = mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

// newAppTestClient gives a second, independently authenticated client against
// the same server so one test can compare what two roles are shown.
func newAppTestClient(t *testing.T, s *appTestServer) *appTestServer {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	clone := *s
	clone.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &clone
}

// probeApp builds an App over the test server's store without going through
// New, so the pure plumbing helpers can be exercised directly.
func (s *appTestServer) probeApp() *App {
	s.t.Helper()
	manager, err := auth.New(s.cfg, s.st)
	if err != nil {
		s.t.Fatal(err)
	}
	return &App{cfg: s.cfg, st: s.st, auth: manager, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// ---- Reading pages ----

// mustContain fails the test at the first wanted string the body lacks.
// what names the page in the failure message.
func mustContain(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q:\n%s", what, want, body)
		}
	}
}

// mustNotContain fails the test at the first unwanted string the body holds.
func mustNotContain(t *testing.T, what, body string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(body, bad) {
			t.Fatalf("%s must not say %q:\n%s", what, bad, body)
		}
	}
}

// between returns the markup from open up to (not including) the first close
// after it, so an assertion about one element is made about that element rather
// than the whole page. It fails the test when either marker is missing.
func between(t *testing.T, body, open, close string) string {
	t.Helper()
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("markup %q not found", open)
	}
	rest := body[start:]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("markup %q has no %q", open, close)
	}
	return rest[:end]
}

// cut returns the text from the first from up to and including the next to
// after it. Unlike between it never fails the test: it returns "" when from is
// missing (tests use that to assert something is absent) and the rest of the
// body when to is missing.
func cut(body, from, to string) string {
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest[len(from):], to); j >= 0 {
		return rest[:len(from)+j+len(to)]
	}
	return rest
}

// requestRevisionFromPage reads the hidden revision field a request screen
// carries, which every change to the request must post back.
func requestRevisionFromPage(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`name="revision" value="([^"]+)"`).FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatal("edit form lacks revision")
	}
	return m[1]
}

// notificationTitles is every in-app notification addressed to one user,
// newest first, as "event|title". Tests check notifications through the store
// because what matters is that the row exists for the right person, not how the
// notification centre renders it.
func notificationTitles(t *testing.T, s *appTestServer, userID int64) []string {
	t.Helper()
	rows, err := s.st.ListNotifications(s.ctx, store.NotificationFilter{UserID: userID, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Event+"|"+row.Title)
	}
	return out
}

// hasEvent reports whether any notificationTitles entry is for this event.
func hasEvent(titles []string, event string) bool {
	for _, entry := range titles {
		if strings.HasPrefix(entry, event+"|") {
			return true
		}
	}
	return false
}

// ---- Small formatters ----

// firstLines shortens a response body for a failure message.
func firstLines(body string) string {
	if len(body) > 400 {
		return body[:400]
	}
	return body
}

// strconvFormat writes an id the way it appears in URLs and form values.
func strconvFormat(id int64) string {
	return strconv.FormatInt(id, 10)
}

// money2 writes paise as the plain rupee amount a form field takes ("1234.50").
func money2(paise int64) string { return fmt.Sprintf("%d.%02d", paise/100, paise%100) }

// currentMonthForTest is the month a settlement recorded now lands in, which is
// the month the payment entry screen tests its lock against.
func currentMonthForTest() string { return time.Now().Format("2006-01") }
