package app

// Payment and request attachments: upload, download, and saving an uploaded
// file into the attachment directory.
//
// Every refusal to hand over an attachment is the same 404 (see
// notFoundAttachment). Attachment ids are small sequential numbers, so a
// different answer for "exists but not yours" would let anyone probe which
// files exist.

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

// notFoundAttachment logs why an attachment was refused and answers with the
// one 404 every refusal gets.
func (a *App) notFoundAttachment(w http.ResponseWriter, r *http.Request, reason string, id int64) {
	a.log.WarnContext(r.Context(), "attachment refused",
		"request_id", requestID(r), "attachment_id", id, "reason", reason)
	a.respondError(w, r, http.StatusNotFound, "The requested attachment was not found.", nil)
}

// attachmentUpload adds a file to a payment. Only someone who may read the
// payment may add to it, because a payment's attachments are the evidence
// that it was made. (The store also refuses attachments on a payment linked
// to a request.)
func (a *App) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
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

// attachmentDownload serves a payment's document (a bank advice, a proof of
// payment) to someone who may read that payment.
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

// requestAttachmentDownload serves a request's own document (the invoice or
// quotation it came in with). Request documents are a separate table with
// their own ids, so this route is separate from /attachments/{id}. The
// document must belong to the request in the path, and that request must be
// in the caller's request scope.
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

// serveAttachmentFile streams a stored attachment as a download named
// originalName. It is shared by both download routes. It refuses (404) any
// stored path outside the attachment directory and anything that is not a
// regular file, and opens the file through os.OpenRoot so the path cannot
// escape that directory.
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

// stageUploadedAttachment saves the form's "attachment" file into the
// attachment directory, before the database row that points at it is
// written. It checks the size limit (the admin setting), the real file type
// (from its content, not its name) and the storage quota.
//
// It returns the attachment's details and the path of the saved file. When
// no file was sent it returns (nil, "", nil). If the caller's database write
// then fails, it must delete the file with removeStagedAttachment.
//
// Uploads take turns (uploadMu) from the type check onwards, so two uploads
// cannot both fit into the last of the quota.
func (a *App) stageUploadedAttachment(r *http.Request) (*store.AttachmentInput, string, error) {
	file, header, err := r.FormFile("attachment")
	if err != nil {
		// No file, or no multipart body to hold one: either way no attachment
		// was sent, which every form that offers one allows.
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
	used, err := attachmentDirUsage(a.cfg.AttachmentDir, quota)
	if err != nil || header.Size > quota-used {
		return nil, "", fmt.Errorf("%w: attachment storage quota reached; contact an administrator", store.ErrValidation)
	}
	if used+header.Size >= quota*8/10 && !a.uploadQuotaWarned {
		a.uploadQuotaWarned = true
		a.log.WarnContext(r.Context(), "attachment storage above 80 percent", "used_bytes", used, "quota_bytes", quota)
	}
	// The user's filename is kept in the database only. On disk the file gets
	// a unique random name (created with mode 0600), which also avoids names
	// too long for the filesystem.
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

// attachmentDirUsage adds up the size of every file under dir. It stops with
// an error as soon as the total would pass quota, or when it finds anything
// other than a regular file or directory.
func attachmentDirUsage(dir string, quota int64) (int64, error) {
	var used int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("attachment directory contains a non-regular file")
		}
		if info.Size() > quota-used {
			return errors.New("attachment storage quota reached")
		}
		used += info.Size()
		return nil
	})
	return used, err
}
