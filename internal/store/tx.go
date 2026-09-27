package store

import (
	"context"
	"database/sql"
)

// The store has one rule for transactions: a write transaction takes SQLite's
// write lock the moment it begins, and a transaction that only reads says so.
//
// Open puts _txlock=immediate on the DSN, so every BeginTx that is not marked
// ReadOnly runs BEGIN IMMEDIATE. Two writers then queue on busy_timeout instead
// of the second failing with SQLITE_BUSY, which is what happened when a
// transaction read first and asked for the lock second. A read-only transaction
// must be marked ReadOnly or it would take the write lock and hold every writer
// up for as long as it runs. Use these two helpers rather than s.db.BeginTx so
// the choice is always explicit.

// beginWriteTx opens a transaction that may write. It holds the write lock from
// BEGIN, so the writer can read and then write without racing anyone.
func (s *Store) beginWriteTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// beginReadTx opens a transaction that only reads, for callers that need one
// consistent snapshot across several queries. It never takes the write lock.
func (s *Store) beginReadTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
}
