package app

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"fervidbudget/internal/store"
)

// Use the same bounded value for the form and the upload. Invalid legacy
// settings fall back to the seeded default; configuration rejects new ones.
func normalizedAttachmentLimit(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 1 || n > maxAttachmentMB {
		return 10
	}
	return n
}

func (a *App) attachmentLimitMB(r *http.Request) (int64, error) {
	raw, err := a.st.AppSetting(r.Context(), "attachment_max_mb")
	if err != nil {
		return 0, fmt.Errorf("read attachment size limit: %w", err)
	}
	return normalizedAttachmentLimit(raw), nil
}

// A browser's accept attribute and its declared Content-Type are hints, not
// validation. Check both the filename and actual content before staging a file.
func validatedAttachmentType(file io.ReadSeeker, filename string) (string, error) {
	expected := map[string]string{".pdf": "application/pdf", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png"}[strings.ToLower(filepath.Ext(filename))]
	invalid := func() (string, error) {
		return "", fmt.Errorf("%w: attach a PDF, JPG or PNG file whose contents match its extension. Choose the attachment again after correcting it", store.ErrValidation)
	}
	if expected == "" {
		return invalid()
	}
	var prefix [512]byte
	n, err := file.Read(prefix[:])
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read attachment content: %w", err)
	}
	actual := http.DetectContentType(prefix[:n])
	if actual != expected {
		return invalid()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind attachment: %w", err)
	}
	if strings.HasPrefix(actual, "image/") {
		if _, _, err := image.DecodeConfig(file); err != nil {
			return invalid()
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind attachment: %w", err)
		}
	}
	return actual, nil
}
