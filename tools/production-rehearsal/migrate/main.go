// Command migrate opens ONLY an explicitly allowlisted local rehearsal database.
// It deliberately imports no HTTP application, notifier, scheduler, or seed code.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fervidbudget/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const root = "/Users/narendhupati/Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26"

func main() {
	path := flag.String("db", "", "exact local rehearsal database path")
	hashPassword := flag.Bool("hash-password", false, "hash a synthetic QA password from stdin without opening any database")
	flag.Parse()
	if *hashPassword {
		if *path != "" {
			fail("hash_mode_no_database")
		}
		password, err := io.ReadAll(io.LimitReader(os.Stdin, 74))
		if err != nil || len(password) < 12 || len(password) > 72 {
			fail("synthetic_password_length")
		}
		hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		if err != nil {
			fail("synthetic_password_hash")
		}
		fmt.Println(string(hash))
		return
	}
	if err := validate(*path); err != nil {
		fail("path_validation")
	}
	result := map[string]any{"path_allowlist_pass": true}
	s, err := store.Open(*path)
	if err != nil {
		fail("first_migration_open")
	}
	var version int
	if err = s.DB().QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 20 {
		s.Close()
		fail("version_20")
	}
	if !integrity(s.DB()) {
		s.Close()
		fail("integrity_first_open")
	}
	first, err := digest(s.DB())
	if err != nil {
		s.Close()
		fail("first_logical_digest")
	}
	if err = s.Close(); err != nil {
		fail("first_close")
	}
	// A separate open proves that normal startup does not apply changes again.
	s, err = store.Open(*path)
	if err != nil {
		fail("second_open")
	}
	second, err := digest(s.DB())
	if err != nil {
		s.Close()
		fail("second_logical_digest")
	}
	ok := integrity(s.DB())
	if err = s.Close(); err != nil {
		fail("second_close")
	}
	if first != second || !ok {
		fail("idempotence_or_integrity")
	}
	result["schema_version"] = version
	result["full_integrity_pass"] = true
	result["foreign_keys_pass"] = true
	result["second_open_schema_and_all_rows_match"] = true
	result["http_seed_mail_scheduler_started"] = false
	json.NewEncoder(os.Stdout).Encode(result)
}

func fail(stage string) {
	// SQLite errors can contain stored values. Never print the underlying error.
	json.NewEncoder(os.Stderr).Encode(map[string]any{"pass": false, "failed_stage": stage})
	os.Exit(1)
}

func validate(path string) error {
	if path != filepath.Join(root, "migrated/data/fervid.db") && path != filepath.Join(root, "synthetic/data/fervid.db") {
		return fmt.Errorf("not allowlisted")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("root permissions")
	}
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("missing or symlink")
		}
		if p == string(filepath.Separator) {
			break
		}
	}
	i, err := os.Stat(path)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("database permissions")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if i, e := os.Lstat(path + suffix); e == nil && (i.Mode()&os.ModeSymlink != 0 || !i.Mode().IsRegular()) {
			return fmt.Errorf("unsafe sidecar")
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}

func integrity(db *sql.DB) bool {
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return false
	}
	n := 0
	for rows.Next() {
		var v string
		if rows.Scan(&v) != nil || v != "ok" {
			rows.Close()
			return false
		}
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || n != 1 {
		return false
	}
	rows, err = db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return false
	}
	defer rows.Close()
	return !rows.Next() && rows.Err() == nil
}

func digest(db *sql.DB) ([32]byte, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	rows, err := db.Query("SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name")
	if err != nil {
		return [32]byte{}, err
	}
	var tables []string
	for rows.Next() {
		var typ, name, table string
		var ddl sql.NullString
		if err = rows.Scan(&typ, &name, &table, &ddl); err != nil {
			rows.Close()
			return [32]byte{}, err
		}
		enc.Encode([]any{typ, name, table, ddl})
		if typ == "table" {
			tables = append(tables, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return [32]byte{}, err
	}
	for _, table := range tables {
		q := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		rows, err = db.Query("SELECT * FROM " + q + " ORDER BY rowid")
		if err != nil {
			return [32]byte{}, err
		}
		cols, e := rows.Columns()
		if e != nil {
			rows.Close()
			return [32]byte{}, e
		}
		enc.Encode([]any{table, cols})
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if e = rows.Scan(ptrs...); e != nil {
				rows.Close()
				return [32]byte{}, e
			}
			enc.Encode(vals)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return [32]byte{}, err
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
