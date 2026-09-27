package store

import "context"

// DetailedBudgetHeads distinguishes explicit line plans from legacy amounts.
func (s *Store) DetailedBudgetHeads(ctx context.Context, month string) (map[int64]bool, error) {
	out := map[int64]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT b.head_id FROM budget_lines l JOIN budgets b ON b.id=l.budget_id WHERE b.month=?`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
