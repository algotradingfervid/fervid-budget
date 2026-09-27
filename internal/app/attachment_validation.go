package app

// Checks and storage helpers for uploaded attachments (PDF, JPG, PNG): the
// configured size limit, content checks that do not trust the browser, image
// re-encoding, and cleanup of a staged file after a failed request.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"fervidbudget/internal/store"
)

// maxAttachmentMB is the largest attachment size limit an admin may set.
const maxAttachmentMB int64 = 20

// normalizedAttachmentLimit turns the attachment_max_mb setting into the
// limit in MiB used by both the form and the upload. A missing or out-of-range
// value (possible only in old data; Configuration refuses new ones) means the
// default of 10.
func normalizedAttachmentLimit(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 1 || n > maxAttachmentMB {
		return 10
	}
	return n
}

// attachmentLimitMB reads the attachment size limit in MiB from settings.
func (a *App) attachmentLimitMB(r *http.Request) (int64, error) {
	raw, err := a.st.AppSetting(r.Context(), "attachment_max_mb")
	if err != nil {
		return 0, fmt.Errorf("read attachment size limit: %w", err)
	}
	return normalizedAttachmentLimit(raw), nil
}

// validatedAttachmentType checks an upload before it is stored and returns its
// MIME type. The browser's file filter and Content-Type are only hints, so the
// extension must be .pdf, .jpg, .jpeg or .png, the file's first bytes must
// match that type, an image must decode (at most 16 megapixels) and a PDF
// must pass strict validation. Any failure is a validation error asking for a
// matching file. The file is rewound before returning.
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
		cfg, _, err := image.DecodeConfig(file)
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
			return invalid()
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind attachment: %w", err)
		}
		if _, _, err = image.Decode(file); err != nil {
			return invalid()
		}
	} else {
		data, err := io.ReadAll(io.LimitReader(file, (20<<20)+1))
		if err != nil || len(data) > 20<<20 || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
			return invalid()
		}
		conf := &model.Configuration{ValidationMode: model.ValidationStrict, Offline: true}
		if err = api.Validate(bytes.NewReader(data), conf); err != nil {
			return invalid()
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind attachment: %w", err)
	}
	return actual, nil
}

// copyAttachment writes the upload to out, at most limit bytes (a PDF is
// copied with one extra byte allowed so the caller can detect an oversized
// file). Images are decoded and re-encoded, so metadata and any data hidden
// after the image never reach storage.
func copyAttachment(out io.Writer, in io.ReadSeeker, mimeType string, limit int64) (int64, error) {
	if !strings.HasPrefix(mimeType, "image/") {
		return io.Copy(out, io.LimitReader(in, limit+1))
	}
	img, _, err := image.Decode(in)
	if err != nil {
		return 0, err
	}
	counter := &attachmentWriter{out: out, limit: limit}
	if mimeType == "image/png" {
		err = png.Encode(counter, img)
	} else {
		err = jpeg.Encode(counter, img, &jpeg.Options{Quality: 90})
	}
	return counter.n, err
}

// attachmentWriter passes writes through to out and fails once more than
// limit bytes would be written.
type attachmentWriter struct {
	out      io.Writer
	n, limit int64
}

// Write implements io.Writer with the size limit.
func (w *attachmentWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-w.n {
		return 0, fmt.Errorf("encoded image exceeds upload limit")
	}
	n, e := w.out.Write(p)
	w.n += int64(n)
	return n, e
}

// removeStagedAttachment deletes a file saved for a request that then
// failed. An empty path or an already missing file is fine; any other error
// is logged.
func removeStagedAttachment(logger *slog.Logger, r *http.Request, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.ErrorContext(r.Context(), "attachment cleanup failed",
			"request_id", requestID(r),
			"path", path,
			"error", err,
		)
	}
}
