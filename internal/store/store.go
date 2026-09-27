package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db            *sql.DB
	badgeRevision atomic.Uint64
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if path != ":memory:" {
		info, err := os.Lstat(path)
		if err == nil && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("database must be a regular file")
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		err = f.Chmod(0600)
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	// Connection settings ride on the DSN so that every pooled connection gets
	// them, not just the one that happened to run a db.Exec("PRAGMA ...").
	//
	// _txlock=immediate is the store's one transaction rule: every write
	// transaction takes the write lock at BEGIN and waits on busy_timeout for it,
	// so two writers queue instead of the second failing with SQLITE_BUSY. A
	// transaction that only reads must say so (beginReadTx), or it would take the
	// write lock too.
	//
	// journal_mode(WAL) lets readers and the writer run side by side. In the
	// default rollback-journal mode a COMMIT that gave up waiting for readers left
	// its transaction open on a pooled connection and the whole app stopped
	// writing until a restart.
	db, err := sql.Open("sqlite", path+"?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// Should a connection ever be left holding a transaction again, retiring it
	// after a while bounds the damage. Closing a connection rolls its transaction
	// back.
	db.SetConnMaxLifetime(5 * time.Minute)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := installFinancialLimits(db); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0600); err != nil && !os.IsNotExist(err) {
			db.Close()
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

// rowScanner is what *sql.Row and *sql.Rows have in common, so one scan
// function can read a single-row lookup and a list alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// rowQuerier is the read surface *sql.DB and *sql.Tx have in common, so a
// validation helper can run inside its caller's transaction instead of going out
// to a second pooled connection. Reading on a second connection while the first
// holds a write transaction is what turned one slow payment into four stacked
// five-second busy waits.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validMonth(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "financial limit") {
		return fmt.Errorf("%w: financial limit exceeded", ErrValidation)
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	// F-B-06 / F-G-027: a FOREIGN KEY violation means a submitted id names a
	// row that does not exist — a client error, not a server fault, so it must
	// reach the user as a 400 that keeps their typed form, never a 500. The
	// driver text is deliberately not echoed. SQLite's message is stable:
	// "constraint failed: FOREIGN KEY constraint failed (787)".
	if strings.Contains(err.Error(), "FOREIGN KEY") {
		return fmt.Errorf("%w: a referenced record does not exist", ErrValidation)
	}
	return err
}

func nullJSON(b []byte) any {
	if string(b) == "null" {
		return nil
	}
	return string(b)
}
