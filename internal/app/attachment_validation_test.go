package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

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

func TestAttachmentValidationMatchesContentAndConfiguredLimit(t *testing.T) {
	s := newAppTestServer(t)
	a := s.probeApp()
	pdf := []byte(validTestPDF)
	var pngBody, jpgBody bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&pngBody, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpgBody, img, nil); err != nil {
		t.Fatal(err)
	}
	boundary := append(append([]byte{}, pdf...), bytes.Repeat([]byte(" "), (10<<20)-len(pdf))...)
	for _, tc := range []struct {
		name, filename, mime string
		data                 []byte
		valid                bool
	}{
		{"pdf", "receipt.PDF", "application/pdf", pdf, true},
		{"jpg", "receipt.jpg", "image/jpeg", jpgBody.Bytes(), true},
		{"jpeg", "receipt.jpeg", "image/jpeg", jpgBody.Bytes(), true},
		{"png", "receipt.png", "image/png", pngBody.Bytes(), true},
		{"text", "receipt.txt", "", []byte("receipt"), false},
		{"text disguised as pdf", "receipt.pdf", "", []byte("receipt"), false},
		{"pdf disguised as jpg", "receipt.jpg", "", pdf, false},
		{"truncated png", "receipt.png", "", []byte("\x89PNG\r\n\x1a\n"), false},
		{"empty pdf", "receipt.pdf", "", nil, false},
		{"exact limit", "receipt.pdf", "application/pdf", boundary, true},
		{"one byte over limit", "receipt.pdf", "", append(append([]byte{}, boundary...), ' '), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ct := attachmentMultipart(t, nil, tc.filename, tc.data)
			r := httptest.NewRequest(http.MethodPost, "/requests", body)
			r.Header.Set("Content-Type", ct)
			att, path, err := a.stageUploadedAttachment(r)
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
			if tc.valid {
				if err != nil || att == nil {
					t.Fatalf("valid attachment rejected: %v", err)
				}
				if att.MimeType != tc.mime || att.SizeBytes != int64(len(tc.data)) {
					t.Fatalf("incorrect attachment metadata: %+v", att)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, store.ErrValidation) || att != nil || path != "" {
				t.Fatalf("invalid attachment accepted: %+v %s %v", att, path, err)
			}
			files, err := os.ReadDir(s.cfg.AttachmentDir)
			if err != nil || len(files) != 0 {
				t.Fatalf("orphan files: %v %v", files, err)
			}
		})
	}
	admin := mustUser(t, s, 1)
	if err := s.st.SetAppSetting(s.ctx, admin, "attachment_max_mb", "1"); err != nil {
		t.Fatal(err)
	}
	body, ct := attachmentMultipart(t, nil, "large.pdf", boundary[:(1<<20)+1])
	r := httptest.NewRequest(http.MethodPost, "/requests", body)
	r.Header.Set("Content-Type", ct)
	_, _, err := a.stageUploadedAttachment(r)
	if !errors.Is(err, store.ErrValidation) || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("configured lower limit ignored: %v", err)
	}
	if r.MultipartForm != nil {
		r.MultipartForm.RemoveAll()
	}
}

func TestRejectedRequestAttachmentRetainsInputWithoutSavingChanges(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Attachment validation")
	manager := seedSecondApprover(t, s)
	requester := s.seedRequester("upload@example.test", "Upload Requester", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Original request", ProjectID: 1, HeadID: headID, Amount: 12000, Purpose: "original purpose", ExpenseDate: "2026-07-17", ManagerID: manager})
	if err != nil {
		t.Fatal(err)
	}
	s.login("upload@example.test", "RequesterPass123")
	fields := url.Values{"csrf": {s.csrf()}, "type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Retained attachment correction"}, "project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"234.56"}, "purpose": {"Keep this typed purpose"}, "expense_date": {"2026-07-18"}, "manager_id": {strconvFormat(manager)}}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"bad.txt", []byte("unsupported")},
		{"oversized.pdf", append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte(" "), 11<<20)...)},
	} {
		for _, path := range []string{fmt.Sprintf("/requests/%d/edit", id), "/requests"} {
			if strings.HasSuffix(path, "/edit") {
				fields.Set("revision", requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, path, nil, ""))))
			}
			body, ct := attachmentMultipart(t, fields, tc.name, tc.data)
			resp := s.request(http.MethodPost, path, body, ct)
			requireStatus(t, resp, http.StatusBadRequest)
			page := responseBody(t, resp)
			for _, want := range []string{"Retained attachment correction", "Keep this typed purpose", `value="234.56"`, "Choose the attachment again"} {
				if !strings.Contains(page, want) {
					t.Fatalf("rejection lost %q", want)
				}
			}
			current, err := s.st.Request(s.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if current.Amount != 12000 || current.ShortTitle != "Original request" || current.Purpose != "original purpose" {
				t.Fatalf("invalid upload partially saved edit: %+v", current)
			}
			var count int
			if err := s.st.DB().QueryRow(`SELECT count(*) FROM payment_requests`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("invalid upload created request: %d %v", count, err)
			}
			if err := s.st.DB().QueryRow(`SELECT count(*) FROM request_attachments`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid upload attached file: %d %v", count, err)
			}
			files, err := os.ReadDir(s.cfg.AttachmentDir)
			if err != nil || len(files) != 0 {
				t.Fatalf("invalid upload left orphan: %v %v", files, err)
			}
		}
	}
	page := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", id), nil, ""))
	if !strings.Contains(page, "up to 10 MiB") || !strings.Contains(page, `accept=".pdf,.jpg,.jpeg,.png"`) {
		t.Fatal("upload control does not advertise the enforced policy")
	}
}

func TestAttachmentLimitConfigurationRejectsUnsupportedValues(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, raw := range []string{"0", "-1", "21", "1.5", "invalid", ""} {
		resp := s.postForm("/configuration", url.Values{"attachment_max_mb": {raw}, "number_prefix": {"Must not save"}})
		requireStatus(t, resp, http.StatusBadRequest)
		_ = responseBody(t, resp)
		got, err := s.st.AppSetting(s.ctx, "attachment_max_mb")
		if err != nil || got != "10" {
			t.Fatalf("invalid config persisted: %q %v", got, err)
		}
		prefix, err := s.st.AppSetting(s.ctx, "number_prefix")
		if err != nil || prefix == "Must not save" {
			t.Fatalf("invalid config partially saved: %q %v", prefix, err)
		}
	}
	resp := s.postForm("/configuration", url.Values{"attachment_max_mb": {"20"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	a := s.probeApp()
	body, ct := attachmentMultipart(t, nil, "allowed.pdf", append([]byte(validTestPDF), bytes.Repeat([]byte(" "), 11<<20)...))
	r := httptest.NewRequest(http.MethodPost, "/requests", body)
	r.Header.Set("Content-Type", ct)
	att, path, err := a.stageUploadedAttachment(r)
	if err != nil || att == nil {
		t.Fatalf("configured larger file limit ignored: %v", err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
