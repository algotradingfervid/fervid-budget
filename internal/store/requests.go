package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// settingInTx reads a runtime setting on the caller's transaction, falling back
// to def when the key is absent. Numbering must see the format that is in force
// at the instant the number is reserved, so it reads inside the same tx.
func settingInTx(tx *sql.Tx, key, def string) (string, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows || (err == nil && strings.TrimSpace(v) == "") {
		return def, nil
	}
	return v, err
}

// requestNumberYear resolves the year segment of a request number from the
// number_year_mode setting: "calendar" (2026), "financial" (2025-26, April
// start) or "none" (empty segment).
func requestNumberYear(tx *sql.Tx, now time.Time) (string, error) {
	mode, err := settingInTx(tx, "number_year_mode", "calendar")
	if err != nil {
		return "", err
	}
	switch mode {
	case "none":
		return "", nil
	case "financial":
		start := now.Year()
		if now.Month() < time.April {
			start--
		}
		return fmt.Sprintf("%d-%02d", start, (start+1)%100), nil
	default:
		return now.Format("2006"), nil
	}
}

// NextRequestNumber reserves the next monotonic request number for a year
// inside the caller's transaction and returns it as <prefix>-<year>-NNNNNN.
func NextRequestNumber(tx *sql.Tx, year string) (string, error) {
	prefix, err := settingInTx(tx, "number_prefix", "PR")
	if err != nil {
		return "", err
	}
	widthText, err := settingInTx(tx, "number_width", "6")
	if err != nil {
		return "", err
	}
	width, err := strconv.Atoi(strings.TrimSpace(widthText))
	if err != nil || width < 1 || width > 12 {
		width = 6
	}
	if _, err := tx.Exec(`INSERT INTO request_number_seq(year,last) VALUES(?,0) ON CONFLICT(year) DO NOTHING`, year); err != nil {
		return "", err
	}
	var last int64
	if err := tx.QueryRow(`UPDATE request_number_seq SET last=last+1 WHERE year=? RETURNING last`, year).Scan(&last); err != nil {
		return "", err
	}
	if year == "" {
		return fmt.Sprintf("%s-%0*d", prefix, width, last), nil
	}
	return fmt.Sprintf("%s-%s-%0*d", prefix, year, width, last), nil
}
