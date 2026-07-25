package store

import (
	"context"
	"database/sql"
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
// Every spec here reads a table that exists today. The "approvals" and
// "accounts_queue" badges the nav already references stay dormant until Phase 1
// creates the requests table; adding them is a matter of appending two specs.
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
		Key: "my_payments", Resource: "payment", Action: "read", PerUser: true,
		Expr: `SELECT COUNT(*) FROM payments WHERE voided_at IS NULL AND entered_by = ?`,
	},
	{
		Key: "receipts_missing", Resource: "payment_attachment", Action: "create",
		Expr: `SELECT COUNT(*) FROM payments p WHERE p.voided_at IS NULL
		         AND NOT EXISTS (SELECT 1 FROM payment_attachments a WHERE a.payment_id = p.id)`,
	},
	{
		Key: "open_months", Resource: "budget", Action: "read",
		Expr: `SELECT COUNT(*) FROM budget_months WHERE status = 'open'`,
	},
}

type badgeCacheKey struct {
	db    *sql.DB
	user  int64
	specs string
}

type badgeCacheEntry struct {
	at     time.Time
	counts map[string]int
}

// badgeCache holds immutable entries only: a stored map is never written to
// again and callers always receive a copy, so concurrent shell renders share
// it without locking beyond the sync.Map itself.
var badgeCache sync.Map

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

	key := badgeCacheKey{db: s.db, user: userID, specs: badgeFingerprint(specs)}
	if cached, ok := badgeCache.Load(key); ok {
		if entry, ok := cached.(badgeCacheEntry); ok && time.Since(entry.at) < badgeCacheTTL {
			return copyCounts(entry.counts), nil
		}
	}

	var query strings.Builder
	query.WriteString("SELECT ")
	args := make([]any, 0, len(specs))
	for i, spec := range specs {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString("(")
		query.WriteString(spec.Expr)
		query.WriteString(")")
		if spec.PerUser {
			args = append(args, userID)
		}
	}

	values := make([]int, len(specs))
	targets := make([]any, len(specs))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := s.db.QueryRowContext(ctx, query.String(), args...).Scan(targets...); err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(specs))
	for i, spec := range specs {
		counts[spec.Key] = values[i]
	}
	badgeCache.Store(key, badgeCacheEntry{at: time.Now(), counts: counts})
	return copyCounts(counts), nil
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

func copyCounts(counts map[string]int) map[string]int {
	out := make(map[string]int, len(counts))
	for key, value := range counts {
		out[key] = value
	}
	return out
}
