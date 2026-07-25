package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// AppSetting returns the value of a runtime setting, or "" if it is unset.
func (s *Store) AppSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// AppSettings returns every runtime setting. The Configuration screen renders
// from this one map (D6).
func (s *Store) AppSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetAppSetting upserts a single runtime setting and records an audit row.
func (s *Store) SetAppSetting(ctx context.Context, actor User, key, value string) error {
	return s.SetAppSettings(ctx, actor, map[string]string{key: value})
}

// SetAppSettings upserts a batch of settings in one transaction with one audit
// row, so a Configuration save is all-or-nothing (house pattern).
func (s *Store) SetAppSettings(ctx context.Context, actor User, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	before := map[string]any{}
	after := map[string]any{}
	for _, k := range keys {
		var prev string
		switch err := tx.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, k).Scan(&prev); err {
		case nil, sql.ErrNoRows:
		default:
			return err
		}
		before[k] = prev
		after[k] = values[k]
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key,value) VALUES(?,?)
 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, values[k]); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "settings",
		EntityType: "app_setting", Summary: "Updated configuration: " + strings.Join(keys, ", "),
		Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
