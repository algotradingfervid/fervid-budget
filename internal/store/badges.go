// badges.go computes, and briefly caches, the count badges in the navigation.

package store

import (
	"context"
	"database/sql"
	"maps"
	"strings"
	"sync"
	"time"
)

// badgeCacheTTL is the memo window. htmx re-renders the shell on every
// navigation, so a burst of requests from one user must not turn into a burst
// of queries. It is a variable so tests can collapse the window.
var badgeCacheTTL = 15 * time.Second

// badgeSpec is one nav badge: the count, the permission that entitles the
// caller to see it, and the scalar sub-select that produces it.
//
// badgeSpecs covers the simple count badges. The request-queue badges
// (approvals, accounts_queue) need scope-aware counts and are computed in
// internal/app/nav.go.
type badgeSpec struct {
	Key      string
	Resource string
	Action   string
	// Expr is a scalar sub-select. It takes at most one parameter, the
	// caller's user id, and only when PerUser is set.
	Expr    string
	PerUser bool
}

var badgeSpecs = []badgeSpec{
	{
		Key: "my_payments", Resource: "payment", Action: "view", PerUser: true,
		Expr: `SELECT COUNT(*) FROM payments WHERE voided_at IS NULL AND entered_by = ?`,
	},
	{
		Key: "receipts_missing", Resource: "attachment", Action: "create",
		Expr: `SELECT COUNT(*) FROM payments p WHERE p.voided_at IS NULL
		         AND NOT EXISTS (SELECT 1 FROM payment_attachments a WHERE a.payment_id = p.id)`,
	},
	{
		Key: "open_months", Resource: "month", Action: "view",
		Expr: `SELECT COUNT(*) FROM budget_months WHERE status = 'open'`,
	},
}

// badgeCacheKey identifies one memo: the database, the user, the set of badges
// they may see, and the invalidation revision it was computed under.
type badgeCacheKey struct {
	db       *sql.DB
	user     int64
	specs    string
	revision uint64
}

// badgeCacheEntry is one memoised result and when it was computed.
type badgeCacheEntry struct {
	at     time.Time
	counts map[string]int
}

// badgeCache holds immutable entries only: a stored map is never written to
// again and callers always receive a copy, so concurrent shell renders share
// it without locking beyond the sync.Map itself.
var badgeCache sync.Map

// InvalidateBadgeCounts makes the next shell reflect a completed mutation.
// The revision prevents an in-flight reader from restoring an older snapshot.
func (s *Store) InvalidateBadgeCounts() {
	s.badgeRevision.Add(1)
	badgeCache.Range(func(key, _ any) bool {
		if k, ok := key.(badgeCacheKey); ok && k.db == s.db {
			badgeCache.Delete(key)
		}
		return true
	})
}

// BadgeCounts returns the nav badge counts the caller is entitled to see.
//
// Every permitted count is gathered in a single statement built from scalar
// sub-selects — one round trip regardless of how many badges are in play — and
// a sub-select is only included when perms allows it, so an unauthorised count
// is never even computed, let alone rendered. Results are memoised per user for
// badgeCacheTTL.
func (s *Store) BadgeCounts(ctx context.Context, userID int64, perms PermissionSet) (map[string]int, error) {
	specs := make([]badgeSpec, 0, len(badgeSpecs))
	for _, spec := range badgeSpecs {
		if perms == nil || !perms.Can(spec.Resource, spec.Action) {
			continue
		}
		specs = append(specs, spec)
	}
	if len(specs) == 0 {
		return map[string]int{}, nil
	}

	key := badgeCacheKey{db: s.db, user: userID, specs: badgeFingerprint(specs), revision: s.badgeRevision.Load()}
	if cached, ok := badgeCache.Load(key); ok {
		if entry, ok := cached.(badgeCacheEntry); ok && time.Since(entry.at) < badgeCacheTTL {
			return maps.Clone(entry.counts), nil
		}
	}

	parts := make([]string, len(specs))
	args := make([]any, 0, len(specs))
	for i, spec := range specs {
		parts[i] = "(" + spec.Expr + ")"
		if spec.PerUser {
			args = append(args, userID)
		}
	}
	query := "SELECT " + strings.Join(parts, ", ")

	values := make([]int, len(specs))
	targets := make([]any, len(specs))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(targets...); err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(specs))
	for i, spec := range specs {
		counts[spec.Key] = values[i]
	}
	if key.revision == s.badgeRevision.Load() {
		badgeCache.Store(key, badgeCacheEntry{at: time.Now(), counts: counts})
	}
	return maps.Clone(counts), nil
}

// badgeFingerprint keeps permission changes from reading a memo built for a
// different set of grants.
func badgeFingerprint(specs []badgeSpec) string {
	keys := make([]string, 0, len(specs))
	for _, spec := range specs {
		keys = append(keys, spec.Key)
	}
	return strings.Join(keys, ",")
}
