package app

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fervidbudget/internal/store"
)

func TestLongAttachmentNamesSurviveCreateEditAndDownload(t *testing.T) {
	s := newAppTestServer(t)
	_, head := s.seedHead("Long filenames")
	manager := seedSecondApprover(t, s)
	requester := s.seedRequester("long-file@example.test", "Long file requester", "RequesterPass123")
	s.login(requester.Email, "RequesterPass123")
	pdf := []byte(validTestPDF)
	names := []string{strings.Repeat("r", 240) + ".pdf", strings.Repeat("文", 82) + ".pdf"}
	fields := url.Values{"csrf": {s.csrf()}, "type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Long original filename"}, "project_id": {"1"}, "head_id": {strconvFormat(head)}, "amount": {"23.45"}, "purpose": {"Keep the upload and its exact filename"}, "expense_date": {"2026-07-17"}, "manager_id": {strconvFormat(manager)}}
	var id int64
	for i, name := range names {
		path := "/requests"
		if i > 0 {
			path = fmt.Sprintf("/requests/%d/edit", id)
			fields.Set("short_title", "Unicode filename correction")
			fields.Set("revision", requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, path, nil, ""))))
		}
		body, ct := attachmentMultipart(t, fields, name, pdf)
		response := s.request(http.MethodPost, path, body, ct)
		requireStatus(t, response, http.StatusSeeOther)
		_ = responseBody(t, response)
		if i == 0 {
			requests, err := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "own", ViewerID: requester.ID})
			if err != nil || len(requests) != 1 {
				t.Fatalf("created request: %v %v", requests, err)
			}
			id = requests[0].ID
		}
		atts, err := s.st.RequestAttachments(s.ctx, id)
		if err != nil || len(atts) != i+1 {
			t.Fatalf("attachments: %v %v", atts, err)
		}
		var found bool
		for _, att := range atts {
			if att.OriginalName == name {
				found = true
				if len(filepath.Base(att.StoredPath)) > 64 {
					t.Fatalf("stored filename is derived from the user's name: %s", att.StoredPath)
				}
				stat, err := os.Stat(att.StoredPath)
				if err != nil || stat.Mode().Perm() != 0600 {
					t.Fatalf("staged file mode: %v %v", stat, err)
				}
				response := s.request(http.MethodGet, fmt.Sprintf("/requests/%d/attachments/%d", id, att.ID), nil, "")
				requireStatus(t, response, http.StatusOK)
				_, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
				if err != nil || params["filename"] != name {
					t.Fatalf("download lost original filename: %v %v", params, err)
				}
				if content := responseBody(t, response); content != string(pdf) {
					t.Fatalf("download changed content: %q", content)
				}
			}
		}
		if !found {
			t.Fatalf("original filename not retained: %q", name)
		}
	}
	files, err := os.ReadDir(s.cfg.AttachmentDir)
	if err != nil || len(files) != 2 {
		t.Fatalf("unexpected staged files: %v %v", files, err)
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil || req.ShortTitle != "Unicode filename correction" || req.Amount != 2345 {
		t.Fatalf("edit failed: %+v %v", req, err)
	}
	// A rejected edit must remove its validly staged long-name file as well.
	fields.Set("purpose", "")
	fields.Set("revision", requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", id), nil, ""))))
	body, ct := attachmentMultipart(t, fields, names[0], pdf)
	response := s.request(http.MethodPost, fmt.Sprintf("/requests/%d/edit", id), body, ct)
	requireStatus(t, response, http.StatusBadRequest)
	_ = responseBody(t, response)
	files, err = os.ReadDir(s.cfg.AttachmentDir)
	if err != nil || len(files) != 2 {
		t.Fatalf("rejected edit left orphan files: %v %v", files, err)
	}
	atts, err := s.st.RequestAttachments(s.ctx, id)
	if err != nil || len(atts) != 2 {
		t.Fatalf("rejected edit attached file: %v %v", atts, err)
	}
}

func TestConcurrentSameOriginalAttachmentNameGetsDistinctPrivateFiles(t *testing.T) {
	s := newAppTestServer(t)
	a := s.probeApp()
	const workers = 24
	name := strings.Repeat("x", 240) + ".pdf"
	type result struct {
		path string
		err  error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		body, ct := attachmentMultipart(t, nil, name, []byte(validTestPDF))
		content := append([]byte(nil), body.Bytes()...)
		wg.Add(1)
		go func(raw []byte, contentType string) {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPost, "/requests", bytes.NewReader(raw))
			r.Header.Set("Content-Type", contentType)
			att, path, err := a.stageUploadedAttachment(r)
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
			if err == nil && (att == nil || att.OriginalName != name || att.MimeType != "application/pdf") {
				err = fmt.Errorf("incorrect attachment metadata: %+v", att)
			}
			results <- result{path, err}
		}(content, ct)
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if seen[result.path] {
			t.Errorf("concurrent uploads share %s", result.path)
		}
		seen[result.path] = true
		stat, err := os.Stat(result.path)
		if err != nil || stat.Mode().Perm() != 0600 {
			t.Errorf("file not private: %v %v", stat, err)
		}
		if err := os.Remove(result.path); err != nil {
			t.Error(err)
		}
	}
	if len(seen) != workers {
		t.Fatalf("unique file count=%d,want%d", len(seen), workers)
	}
	files, err := os.ReadDir(s.cfg.AttachmentDir)
	if err != nil || len(files) != 0 {
		t.Fatalf("cleanup left files: %v %v", files, err)
	}
}
