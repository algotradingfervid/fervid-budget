package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UpPasswordReset stores only token hashes. Throttle keys are also hashed so
// unknown account addresses and source addresses need not be retained.
func UpPasswordReset(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS password_resets (
 token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS password_reset_throttle (key_hash TEXT PRIMARY KEY, window_start INTEGER NOT NULL, attempts INTEGER NOT NULL);`)
	return err
}

func (s *Store) AllowPasswordReset(ctx context.Context, keys []string, now time.Time) (bool, error) {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM password_reset_throttle WHERE window_start<?`, now.Add(-time.Hour).Unix()); err != nil {
		return false, err
	}
	allowed := true
	for _, key := range keys {
		if _, err = tx.ExecContext(ctx, `INSERT INTO password_reset_throttle(key_hash,window_start,attempts) VALUES(?,?,1) ON CONFLICT(key_hash) DO UPDATE SET attempts=attempts+1`, key, now.Unix()); err != nil {
			return false, err
		}
		var attempts int
		if err = tx.QueryRowContext(ctx, `SELECT attempts FROM password_reset_throttle WHERE key_hash=?`, key).Scan(&attempts); err != nil {
			return false, err
		}
		if attempts > 5 {
			allowed = false
		}
	}
	return allowed, tx.Commit()
}

func (s *Store) CreatePasswordReset(ctx context.Context, userID int64, hash string, now time.Time) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT active FROM users WHERE id=?`, userID).Scan(&active); err != nil {
		return err
	}
	if active != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=? OR expires_at<=?`, userID, now.Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO password_resets(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`, hash, userID, now.Add(30*time.Minute).Unix(), now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConsumePasswordReset(ctx context.Context, tokenHash, passwordHash string, now time.Time) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var u User
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.name FROM password_resets r JOIN users u ON u.id=r.user_id WHERE r.token_hash=? AND r.expires_at>? AND u.active=1`, tokenHash, now.Unix()).Scan(&u.ID, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// The delete claims the token in this same transaction; a concurrent reuse
	// must either fail to acquire the write or observe the consumed token.
	result, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE token_hash=?`, tokenHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET session_version=session_version+1,password_hash=?,attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, passwordHash, u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=?`, u.ID); err != nil {
		return err
	}
	if err = recordAuditTx(ctx, tx, AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "password_reset", EntityType: "user", EntityID: &u.ID, Summary: "Password reset using a one-time email link"}); err != nil {
		return err
	}
	return tx.Commit()
}

// UpSessionRevocation adds a durable generation so reactivation cannot revive
// a cookie issued before the account was disabled. Existing users begin at zero.
func UpSessionRevocation(tx *sql.Tx) error {
	exists, err := columnExists(tx, "users", "session_version")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = tx.Exec(`ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 0`)
	return err
}

// RevokeSessions makes every previously issued cookie for this user invalid.
func (s *Store) RevokeSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=?`, userID)
	return err
}
func (s *Store) ValidPasswordReset(ctx context.Context, hash string, now time.Time) (bool, error) {
	var valid bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM password_resets r JOIN users u ON u.id=r.user_id WHERE token_hash=? AND expires_at>? AND u.active=1)`, hash, now.Unix()).Scan(&valid)
	return valid, err
}
