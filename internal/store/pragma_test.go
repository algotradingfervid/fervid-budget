package store

import (
	"path/filepath"
	"sync"
	"testing"
)

// SQLite PRAGMA state is per-connection, and database/sql opens connections on
// demand. Setting the pragmas with a single db.Exec after Open therefore
// configured only the first connection: foreign keys were silently off on every
// other one, so referential integrity held by luck. They now ride on the DSN,
// which modernc.org/sqlite applies to each new connection.
//
// This test fails on the old approach — foreign_keys reads 0 on the pooled
// connections and the dangling insert succeeds.
func TestForeignKeysEnforcedOnEveryPooledConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "fk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.DB().SetMaxOpenConns(8)

	// Force several connections open at once so later inserts land on fresh ones.
	var wg sync.WaitGroup
	hold := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var on int
			if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
				t.Errorf("read pragma: %v", err)
				return
			}
			if on != 1 {
				t.Errorf("foreign_keys = %d on this connection, want 1", on)
			}
			<-hold
		}()
	}
	close(hold)
	wg.Wait()

	// And prove it bites: a child row pointing at a missing parent must fail.
	for i := 0; i < 8; i++ {
		if _, err := s.DB().Exec(
			`INSERT INTO user_roles(user_id, role_id) VALUES(999999, 999999)`); err == nil {
			t.Fatal("insert with a dangling foreign key succeeded; enforcement is off")
		}
	}
}
