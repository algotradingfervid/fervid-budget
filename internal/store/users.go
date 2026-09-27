// users.go creates, reads and saves user accounts, keeps the older users.role
// column in step with the assigned roles, and tracks failed logins.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateUser creates a user and lets the legacy role string decide their first
// role, as it always has. Seeding and the tests use this shape.
func (s *Store) CreateUser(ctx context.Context, email, name, hash, role string, active bool) (int64, error) {
	return s.CreateUserWithRoles(ctx, email, name, hash, role, active, nil)
}

// CreateUserWithRoles creates a user and gives them exactly the roles named.
// With no roles named, the role string decides the first role
// (assignDefaultRoleTx), which is how seeding and tests create users.
func (s *Store) CreateUserWithRoles(ctx context.Context, email, name, hash, role string, active bool, roleIDs []int64) (int64, error) {
	return s.createUserWithRoles(ctx, 0, email, name, hash, role, active, roleIDs)
}

// CreateManagedUser creates a user from the Users screen. At least one role is
// required, and actor may only grant roles within their own permissions (the
// delegation ceiling), checked in the same transaction as the insert.
func (s *Store) CreateManagedUser(ctx context.Context, actor User, email, name, hash string, active bool, roleIDs []int64) (int64, error) {
	if len(roleIDs) == 0 {
		return 0, ErrValidation
	}
	return s.createUserWithRoles(ctx, actor.ID, email, name, hash, "data_entry", active, roleIDs)
}

// createUserWithRoles inserts the user and their roles in one transaction.
// actorID 0 means an internal caller (seeding, tests): no delegation check, and
// role decides users.role. Otherwise users.role follows the granted roles.
func (s *Store) createUserWithRoles(ctx context.Context, actorID int64, email, name, hash, role string, active bool, roleIDs []int64) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if err := validateUserFields(email, name, role); err != nil {
		return 0, err
	}
	if role == "" {
		role = "data_entry"
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if actorID != 0 {
		if err := requireRoleCeiling(ctx, tx, actorID, roleIDs); err != nil {
			return 0, err
		}
	}
	if actorID != 0 {
		if role, err = legacyRoleTx(ctx, tx, roleIDs); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,password_hash,role,active) VALUES(?,?,?,?,?)`,
		email, name, hash, role, boolInt(active))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	wanted := dedupeIDs(roleIDs, true)
	if len(wanted) == 0 {
		if err := assignDefaultRoleTx(ctx, tx, id, role); err != nil {
			return 0, err
		}
	} else {
		for _, rid := range wanted {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`,
				id, rid); err != nil {
				return 0, classify(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// EnsureUser creates the user unless one with this email already exists.
func (s *Store) EnsureUser(ctx context.Context, email, name, hash, role string) error {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email=?`, strings.ToLower(email)).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.CreateUser(ctx, email, name, hash, role, true)
	return err
}

// UserByEmail looks a user up by email (case-insensitive), or ErrNotFound.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return scanUser(row)
}

// UserByID looks a user up by id, or ErrNotFound.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users WHERE id=?`, id)
	return scanUser(row)
}

// ListUsers returns every user, active or not, ordered by name.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser rewrites a user's name, users.role, active flag and (when
// passwordHash is not empty) password, and leaves their roles alone. Only tests
// call it, for example to deactivate a signed-in user. Screens save users with
// SaveUser: UpdateUser checks the last-active-administrator rule outside its
// transaction, so two concurrent calls could both pass it.
func (s *Store) UpdateUser(ctx context.Context, id int64, name, role string, active bool, passwordHash string) error {
	name = strings.TrimSpace(name)
	if err := validateUserFields("placeholder@example.invalid", name, role); err != nil {
		return err
	}
	if err := s.RequireAnotherActiveAdmin(ctx, id, role, active); err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if passwordHash != "" || !active {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=?`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=?`, id); err != nil {
			return err
		}
	}
	// An empty passwordHash keeps the current password.
	if _, err = tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, password_hash=COALESCE(NULLIF(?,''),password_hash), updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, role, boolInt(active), passwordHash, id); err != nil {
		return classify(err)
	}
	return tx.Commit()
}

// UserSaveInput is one submission of the Users screen's edit sheet: the profile,
// the role assignment and the default approver together. They arrive together
// because they are saved together — see SaveUser.
type UserSaveInput struct {
	ID     int64
	Name   string
	Role   string
	Active bool
	// PasswordHash is empty when the sheet was submitted without a new password.
	PasswordHash string
	// RoleIDs replaces the user's entire role assignment. A nil slice clears it,
	// which is what unticking every box means.
	RoleIDs []int64
	// DefaultApproverID is 0 to clear the field.
	DefaultApproverID int64
}

// SaveUser saves the Users screen's edit sheet — profile, roles and default
// approver — and its audit row in one transaction, so a refused save changes
// nothing. Before any write it checks the profile fields, that actor may grant
// these roles (the role ceiling), that every role exists, that the last active
// administrator is not removed, and that nobody is their own default approver.
// A new password or a deactivation also ends the user's sessions and cancels
// their password-reset links.
func (s *Store) SaveUser(ctx context.Context, actor User, in UserSaveInput) error {
	name := strings.TrimSpace(in.Name)
	if err := validateUserFields("placeholder@example.invalid", name, in.Role); err != nil {
		return err
	}
	if in.DefaultApproverID != 0 && in.DefaultApproverID == in.ID {
		return fmt.Errorf("%w: a user cannot be their own default approver", ErrValidation)
	}
	// Zero stays in, so the role check below rejects it as an unknown role.
	roleIDs := dedupeIDs(in.RoleIDs, false)

	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := protectUserChange(ctx, tx, actor.ID, in.ID, roleIDs, in.Active); err != nil {
		return err
	}
	if in.Role, err = legacyRoleTx(ctx, tx, roleIDs); err != nil {
		return err
	}

	// The current profile, read inside the transaction, is the audit "before".
	var currentRole, currentName string
	var currentActive int
	if err := tx.QueryRowContext(ctx, `SELECT role,active,name FROM users WHERE id=?`, in.ID).
		Scan(&currentRole, &currentActive, &currentName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var approver any
	if err := requireDefaultApproverTx(ctx, tx, in.ID, in.DefaultApproverID); err != nil {
		return err
	}
	if in.DefaultApproverID != 0 {
		approver = in.DefaultApproverID
	}

	// An empty PasswordHash keeps the current password.
	if _, err := tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, password_hash=COALESCE(NULLIF(?,''),password_hash), default_approver_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, in.Role, boolInt(in.Active), in.PasswordHash, approver, in.ID); err != nil {
		return classify(err)
	}
	// Administrator password changes and deactivation supersede email reset
	// links already issued. Re-activation must not resurrect an old credential.
	if in.PasswordHash != "" || !in.Active {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=?`, in.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=?`, in.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?`, in.ID); err != nil {
		return err
	}
	for _, id := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, in.ID, id); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "update", "user", &in.ID, "Updated user "+name).withChange(
		map[string]any{"name": currentName, "role": currentRole, "active": currentActive == 1},
		map[string]any{"name": name, "role": in.Role, "active": in.Active,
			"role_ids": roleIDs, "default_approver_id": in.DefaultApproverID})); err != nil {
		return err
	}
	return tx.Commit()
}

// RequireAnotherActiveAdmin prevents disabling or demoting the final active
// administrator. It is deliberately public so callers can surface a useful
// validation message before displaying a confirmation form.
func (s *Store) RequireAnotherActiveAdmin(ctx context.Context, userID int64, nextRole string, nextActive bool) error {
	var currentRole string
	var currentActive int
	if err := s.db.QueryRowContext(ctx, `SELECT role,active FROM users WHERE id=?`, userID).Scan(&currentRole, &currentActive); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentRole != "admin" || currentActive != 1 || (nextRole == "admin" && nextActive) {
		return nil
	}
	var others int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1 AND id<>?`, userID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
	}
	return nil
}

// LoginLocked reports whether a known account remains temporarily locked.
func (s *Store) LoginLocked(ctx context.Context, email string) (bool, time.Time, error) {
	var locked sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT locked FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, time.Time{}, ErrNotFound
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !locked.Valid || !locked.Time.After(time.Now()) {
		return false, time.Time{}, nil
	}
	return true, locked.Time, nil
}

// RecordFailedLogin increments a known user's failure counter and locks the
// account for fifteen minutes after five consecutive failures. It intentionally
// returns no signal for unknown email addresses to avoid account enumeration.
func (s *Store) RecordFailedLogin(ctx context.Context, email string) (time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	var attempts int
	var previousLock sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT attempt_count,locked FROM users WHERE email=?`, email).Scan(&attempts, &previousLock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	if previousLock.Valid && !previousLock.Time.After(time.Now()) {
		attempts = 0
	}
	attempts++
	var locked any
	var until time.Time
	if attempts >= 5 {
		until = time.Now().Add(15 * time.Minute).UTC()
		locked = until
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET attempt_count=?, last_attempt=CURRENT_TIMESTAMP, locked=?, updated_at=CURRENT_TIMESTAMP WHERE email=?`, attempts, locked, email); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return until, nil
}

// ResetLoginFailures is the email-oriented companion for login handlers that
// do not retain a user ID after authentication has succeeded.
func (s *Store) ResetLoginFailures(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return classify(err)
}

// scanUser reads one users row selected as id,email,name,password_hash,role,
// active,created_at,updated_at,default_approver_id,session_version.
func scanUser(scanner rowScanner) (User, error) {
	var u User
	var active int
	var approver sql.NullInt64
	err := scanner.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &active, &u.CreatedAt, &u.UpdatedAt, &approver, &u.SessionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.Active = active == 1
	u.DefaultApproverID = approver.Int64
	return u, err
}

// validateUserFields checks the email, name and users.role value ("" means
// the default).
func validateUserFields(email, name, role string) error {
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: email and name are required", ErrValidation)
	}
	if role != "" && role != "admin" && role != "data_entry" {
		return fmt.Errorf("%w: invalid user role", ErrValidation)
	}
	return nil
}

// dedupeIDs returns ids without repeats, keeping first-seen order. With
// dropZero it also leaves out 0 (an unticked or empty form value).
func dedupeIDs(ids []int64, dropZero bool) []int64 {
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if (dropZero && id == 0) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// legacyRoleTx returns the users.role value these roles imply: "admin" when
// any of them is the system Admin role, otherwise "data_entry". The column is
// kept in step with the roles so it never contradicts them.
func legacyRoleTx(ctx context.Context, tx *sql.Tx, roleIDs []int64) (string, error) {
	role := "data_entry"
	for _, id := range roleIDs {
		var admin bool
		if err := tx.QueryRowContext(ctx, `SELECT is_system=1 AND name='Admin' FROM roles WHERE id=?`, id).Scan(&admin); err != nil {
			return "", err
		}
		if admin {
			role = "admin"
		}
	}
	return role, nil
}
