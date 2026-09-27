// Package store is the only code that talks to the SQLite database. Every read
// and write of budgets, payments, requests, users and settings goes through a
// *Store method, and every change is written together with its audit row.
//
// store.go opens the database and holds the small helpers every other file
// shares. The other files each cover one area: users.go, catalog.go (projects
// and heads), budgets.go (budgets, month plans, month locks), grid.go, payments.go,
// accounts_queue.go (the Accounts reservation queue), audit.go, requests*.go and
// so on. schema.go plus migrations.go define the tables.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// Store is an open database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
	// badgeCache memoises nav badge counts per user (see BadgeCounts), and
	// badgeRevision is bumped on every write so an older snapshot is never
	// stored over a newer one.
	badgeCache    sync.Map
	badgeRevision atomic.Uint64
}

// Open opens (or creates) the SQLite database at path with owner-only file
// permissions, creates the baseline tables, runs the migrations and installs the
// financial-limit triggers. ":memory:" opens a private in-memory database.
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

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying handle, for backups and tests.
func (s *Store) DB() *sql.DB { return s.db }

// rowScanner is what *sql.Row and *sql.Rows have in common, so one scan
// function can read a single-row lookup and a list alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// rowQuerier is the read surface *sql.DB and *sql.Tx have in common, so a
// validation helper can run inside its caller's transaction. A writer must not
// read on a second pooled connection while it holds the write lock: that read
// can wait out the busy timeout behind the writer's own lock.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// querier is rowQuerier plus multi-row queries: what *sql.DB and *sql.Tx
// have in common, so the budget-plan and permission readers can run inside
// the caller's transaction or on their own.
type querier interface {
	rowQuerier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// validDate reports whether s is a "YYYY-MM-DD" date.
func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// validMonth reports whether s is a "YYYY-MM" month.
func validMonth(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

// boolInt stores a bool as SQLite's 0/1.
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// classify turns SQLite constraint failures into the store's sentinel errors so
// handlers answer 400 (with the user's form kept) instead of 500: a tripped
// financial-limit trigger and a foreign-key failure become ErrValidation, a
// UNIQUE failure ErrDuplicate. Other errors pass through unchanged.
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
	// A FOREIGN KEY failure means a submitted id names a row that does not
	// exist: a client error. The driver text is not echoed. SQLite's message is
	// stable: "constraint failed: FOREIGN KEY constraint failed (787)".
	if strings.Contains(err.Error(), "FOREIGN KEY") {
		return fmt.Errorf("%w: a referenced record does not exist", ErrValidation)
	}
	return err
}

// nullJSON stores marshalled JSON, turning the JSON "null" into SQL NULL.
func nullJSON(b []byte) any {
	if string(b) == "null" {
		return nil
	}
	return string(b)
}
