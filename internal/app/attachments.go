package app

// Payment and request attachments: upload, download, and saving an
// uploaded file to the attachment directory.

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// notFoundAttachment is the single refusal for every attachment the caller may
// not have. It is a 404 and not a 403 on purpose: attachment ids are small
// sequential integers, and a distinguishable refusal turns the route into an
// enumeration oracle for the whole table.
func (a *App) notFoundAttachment(w http.ResponseWriter, r *http.Request, reason string, id int64) {
	a.log.WarnContext(r.Context(), "attachment refused",
		"request_id", requestID(r), "attachment_id", id, "reason", reason)
	a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
}

func (a *App) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	// The write path needs the same ownership check the read path needs, in the
	// other direction: the attachments on a payment *are* the evidence that it
	// happened as recorded, and evidence any signed-in user can add to is not
	// evidence (F-A-03). AddAttachment separately refuses a linked payment, which
	// makes this unreachable today; the check stays so the hole does not reopen
	// the day a free-standing payment becomes creatable again.
	payment, err := a.st.Payment(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "payment not found", id)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !a.canReadPayment(r, payment) {
		a.notFoundAttachment(w, r, "payment out of the caller's scope", id)
		return
	}
	attachment, attachmentPath, err := a.stageUploadedAttachment(r)
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		a.respondStoreError(w, r, err)
		return
	}
	if attachment == nil {
		a.respondError(w, r, http.StatusBadRequest, "Choose a file to upload.", nil)
		return
	}
	if err := a.st.AddAttachment(r.Context(), auth.CurrentUser(r), id, attachment.OriginalName, attachment.StoredPath, attachment.MimeType, attachment.SizeBytes); err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// attachmentDownload serves a *payment* document — a bank advice, a proof of
// payment — and only to somebody the payment's own request is visible to.
func (a *App) attachmentDownload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	att, payment, err := a.st.AttachmentWithPayment(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such payment attachment", id)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !a.canReadPayment(r, payment) {
		a.notFoundAttachment(w, r, "payment out of the caller's scope", id)
		return
	}
	a.serveAttachmentFile(w, r, att.ID, att.StoredPath, att.OriginalName, att.MimeType)
}

// requestAttachmentDownload serves a *request* document — the invoice or
// quotation the request came in with. It reads request_attachments, checks the
// document really belongs to the {id} in the path, and scopes it by that
// request. Both halves matter: without the table split the two id sequences
// collide (F-A-05), and without the scope check attachment:view would read
// every request's documents (F-A-01).
func (a *App) requestAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	attachmentID := parseID(r.PathValue("attachmentID"))
	att, err := a.st.RequestAttachmentByID(r.Context(), attachmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such request attachment", attachmentID)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	// The document has to belong to the request in the URL. Otherwise the path's
	// {id} is decoration and the route is /attachments/{id} again under a longer
	// name.
	if att.RequestID != pathID(r) {
		a.notFoundAttachment(w, r, "attachment belongs to another request", attachmentID)
		return
	}
	req, err := a.st.Request(r.Context(), att.RequestID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFoundAttachment(w, r, "no such request", attachmentID)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if !a.canSeeRequest(r, req) {
		a.notFoundAttachment(w, r, "request out of the caller's scope", attachmentID)
		return
	}
	a.serveAttachmentFile(w, r, att.ID, att.StoredPath, att.OriginalName, att.MimeType)
}

// serveAttachmentFile is the path-safety and streaming tail both download
// routes share, so the two can never drift about what is inside AttachmentDir.
func (a *App) serveAttachmentFile(w http.ResponseWriter, r *http.Request, id int64, storedPath, originalName, mimeType string) {
	root, err := filepath.Abs(a.cfg.AttachmentDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The attachment could not be opened.", err)
		return
	}
	stored, err := filepath.Abs(storedPath)
	if err != nil || !strings.HasPrefix(stored, root+string(os.PathSeparator)) {
		a.log.WarnContext(r.Context(), "unsafe attachment path rejected", "request_id", requestID(r), "attachment_id", id)
		a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
		return
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		a.notFoundAttachment(w, r, "attachment root unavailable", id)
		return
	}
	defer dir.Close()
	rel, err := filepath.Rel(root, stored)
	if err != nil {
		a.notFoundAttachment(w, r, "unsafe attachment path", id)
		return
	}
	file, err := dir.Open(rel)
	if err != nil {
		a.notFoundAttachment(w, r, "attachment unavailable", id)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		a.notFoundAttachment(w, r, "attachment unavailable", id)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": originalName}))
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	http.ServeContent(w, r, originalName, info.ModTime(), file)
}

func (a *App) stageUploadedAttachment(r *http.Request) (*store.AttachmentInput, string, error) {
	file, header, err := r.FormFile("attachment")
	if err != nil {
		// No file, or no multipart body to hold one: either way the caller sent
		// no attachment, which is not an error on any of the forms that offer one.
		if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read attachment: %w", err)
	}
	defer file.Close()
	if strings.TrimSpace(header.Filename) == "" {
		return nil, "", nil
	}
	limitMB, err := a.attachmentLimitMB(r)
	if err != nil {
		return nil, "", err
	}
	limit := limitMB << 20
	if header.Size > limit {
		return nil, "", fmt.Errorf("%w: files must be %d MiB or smaller. Choose the attachment again after correcting it", store.ErrValidation, limitMB)
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	mimeType, err := validatedAttachmentType(file, header.Filename)
	if err != nil {
		return nil, "", err
	}
	quota := a.cfg.AttachmentQuotaBytes
	if quota <= 0 {
		quota = 1 << 30
	}
	var used int64
	err = filepath.WalkDir(a.cfg.AttachmentDir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("attachment directory contains a non-regular file")
		}
		if info.Size() > quota-used {
			return fmt.Errorf("attachment storage quota reached")
		}
		used += info.Size()
		return nil
	})
	if err != nil || header.Size > quota-used {
		return nil, "", fmt.Errorf("%w: attachment storage quota reached; contact an administrator", store.ErrValidation)
	}
	if used+header.Size >= quota*8/10 && !a.uploadQuotaWarned {
		a.uploadQuotaWarned = true
		a.log.WarnContext(r.Context(), "attachment storage above 80 percent", "used_bytes", used, "quota_bytes", quota)
	}
	// Keep the user's filename in metadata only. Prefixing it on disk can
	// exceed the filesystem's component limit, even when the original filename
	// is valid. CreateTemp also reserves a distinct 0600 file atomically for
	// concurrent uploads with the same original name.
	out, err := os.CreateTemp(a.cfg.AttachmentDir, "attachment-*")
	if err != nil {
		return nil, "", fmt.Errorf("create attachment: %w", err)
	}
	stored := out.Name()
	cleanup := func() {
		if removeErr := os.Remove(stored); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			a.log.ErrorContext(r.Context(), "attachment cleanup failed", "request_id", requestID(r), "path", stored, "error", removeErr)
		}
	}
	size, copyErr := copyAttachment(out, file, mimeType, min(limit, quota-used))
	cerr := out.Close()
	if copyErr != nil {
		cleanup()
		return nil, "", fmt.Errorf("store attachment: %w", copyErr)
	}
	if cerr != nil {
		cleanup()
		return nil, "", fmt.Errorf("close attachment: %w", cerr)
	}
	if size > limit {
		cleanup()
		return nil, "", fmt.Errorf("%w: files must be %d MiB or smaller", store.ErrValidation, limitMB)
	}
	return &store.AttachmentInput{
		OriginalName: filepath.Base(header.Filename),
		StoredPath:   stored,
		MimeType:     mimeType,
		SizeBytes:    size,
	}, stored, nil
}
