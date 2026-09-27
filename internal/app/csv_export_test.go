package app

import (
	"net/http"
	"strings"
	"testing"
)

// F-G-030 — every CSV amount was a quoted, symbol-bearing, comma-grouped string,
// so Number() of it is NaN and a spreadsheet has to clean the column first.
// F-G-031 — /reports/ytd.csv always exported the head level, whichever tab the
// download was pressed from.
func TestCSVExportsCarryNumbersAndTheLevelTheyWereAskedFor(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Numeric CSV")
	s.loginAdmin()
	s.settleOneRequest(971, headID, 150000, "1,500.00", "settled", "2026-04-15")

	for _, path := range []string{"/export.csv?month=2026-04", "/reports/ytd.csv?from=2026-04&to=2026-04", "/requests/export.csv?bucket=all"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "charset=utf-8") {
			t.Errorf("%s Content-Type = %q, and ₹ is multi-byte", path, ct)
		}
		body := responseBody(t, resp)
		if strings.Contains(body, "₹") {
			t.Errorf("%s still writes a currency symbol into a data column", path)
		}
		if !strings.Contains(body, "1500.00") {
			t.Errorf("%s does not carry the amount as a plain number: %s", path, body)
		}
	}

	// The level is a parameter, and an unknown one falls back to heads rather
	// than reaching store.Report with a mode it does not know.
	monthly := s.get("/reports/ytd.csv?from=2026-04&to=2026-04&level=monthly")
	heads := s.get("/reports/ytd.csv?from=2026-04&to=2026-04&level=heads")
	if monthly == heads {
		t.Fatal("?level= changes nothing, so the export cannot be the tab it was pressed from")
	}
	requireStatus(t, s.request(http.MethodGet, "/reports/ytd.csv?level=nonsense", nil, ""), http.StatusOK)
}

func TestCSVAmountIsANumber(t *testing.T) {
	for _, tc := range []struct {
		paise int64
		want  string
	}{{0, "0.00"}, {5, "0.05"}, {150000, "1500.00"}, {-2350, "-23.50"}, {100, "1.00"}} {
		if got := csvAmount(tc.paise); got != tc.want {
			t.Errorf("csvAmount(%d) = %q, want %q", tc.paise, got, tc.want)
		}
	}
}
