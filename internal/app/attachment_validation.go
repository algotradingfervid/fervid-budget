package app

import (
	"bytes"
	"fmt"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"image"
	"image/jpeg"
	"image/png"
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
		cfg, _, err := image.DecodeConfig(file)
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
			return invalid()
		}
		file.Seek(0, io.SeekStart)
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
		return "", err
	}

	return actual, nil
}

// Re-encode decoded images so appended payloads and metadata never reach storage.
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

type attachmentWriter struct {
	out      io.Writer
	n, limit int64
}

func (w *attachmentWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-w.n {
		return 0, fmt.Errorf("encoded image exceeds upload limit")
	}
	n, e := w.out.Write(p)
	w.n += int64(n)
	return n, e
}
