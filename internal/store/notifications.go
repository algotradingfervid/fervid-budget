package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// MailSettings is the typed view of the handful of app_settings keys the
// notification layer needs.
//
// The plan called this AppSettings with Get/SetAppSettings accessors, but
// Phase 2 already owns `AppSettings(ctx) map[string]string` and
// `SetAppSettings(ctx, actor, map[string]string)` — the generic key/value pair
// the Configuration screen saves through. These are the typed wrapper over a
// named subset of the same table, so they carry their own name.
//
// The SMTP password is deliberately absent: it comes from the environment
// (config.SMTPPassword) and is never persisted. TestMailSettingsNeverStoreA
// Password pins that no app_settings key ever contains one.
type MailSettings struct {
	SMTPHost             string
	SMTPPort             int
	SMTPUsername         string
	SMTPFromName         string
	SMTPFromAddr         string
	ManagementRecipients string
	// BaseURL makes {{link}} absolute. A relative path is useless in a mail
	// client. Empty means "render the bare path", which is what tests and a
	// not-yet-configured install get.
	BaseURL string
}

const (
	keySMTPHost       = "smtp_host"
	keySMTPPort       = "smtp_port"
	keySMTPUsername   = "smtp_username"
	keySMTPFromName   = "smtp_from_name"
	keySMTPFromAddr   = "smtp_from_addr"
	keyManagementList = "management_recipients"
	keyBaseURL        = "base_url"
)

func (s *Store) GetMailSettings(ctx context.Context) (MailSettings, error) {
	all, err := s.AppSettings(ctx)
	if err != nil {
		return MailSettings{}, err
	}
	port, _ := strconv.Atoi(all[keySMTPPort])
	return MailSettings{
		SMTPHost:             all[keySMTPHost],
		SMTPPort:             port,
		SMTPUsername:         all[keySMTPUsername],
		SMTPFromName:         all[keySMTPFromName],
		SMTPFromAddr:         all[keySMTPFromAddr],
		ManagementRecipients: all[keyManagementList],
		BaseURL:              all[keyBaseURL],
	}, nil
}

func (s *Store) SetMailSettings(ctx context.Context, actor User, in MailSettings) error {
	return s.SetAppSettings(ctx, actor, map[string]string{
		keySMTPHost:       strings.TrimSpace(in.SMTPHost),
		keySMTPPort:       strconv.Itoa(in.SMTPPort),
		keySMTPUsername:   strings.TrimSpace(in.SMTPUsername),
		keySMTPFromName:   strings.TrimSpace(in.SMTPFromName),
		keySMTPFromAddr:   strings.TrimSpace(in.SMTPFromAddr),
		keyManagementList: strings.TrimSpace(in.ManagementRecipients),
		keyBaseURL:        strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
	})
}

const notificationSettingSelect = `SELECT event,label,audience,email_enabled,to_recipients,cc_recipients,
	include_requester,include_manager,include_accounts,subject_template,body_template
	FROM notification_settings`

func scanNotificationSetting(sc interface{ Scan(...any) error }) (NotificationSetting, error) {
	var n NotificationSetting
	var email, req, mgr, acct int
	err := sc.Scan(&n.Event, &n.Label, &n.Audience, &email, &n.ToRecipients, &n.CcRecipients,
		&req, &mgr, &acct, &n.SubjectTemplate, &n.BodyTemplate)
	if err == sql.ErrNoRows {
		return n, ErrNotFound
	}
	n.EmailEnabled, n.IncludeRequester, n.IncludeManager, n.IncludeAccounts = email == 1, req == 1, mgr == 1, acct == 1
	return n, err
}

func (s *Store) NotificationSetting(ctx context.Context, event string) (NotificationSetting, error) {
	return scanNotificationSetting(s.db.QueryRowContext(ctx, notificationSettingSelect+` WHERE event=?`, event))
}

// AllNotificationSettings returns every event in the order the admin screen
// lists them, which is the seeded sort_order.
func (s *Store) AllNotificationSettings(ctx context.Context) ([]NotificationSetting, error) {
	rows, err := s.db.QueryContext(ctx, notificationSettingSelect+` ORDER BY sort_order,event`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationSetting
	for rows.Next() {
		n, err := scanNotificationSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SetNotificationSetting updates the editable columns of one event.
//
// label, audience and sort_order are deliberately not in the SET list: they are
// seeded presentation, not admin input, and the edit sheet never posts them. An
// UPDATE (not an upsert) means an unknown event is rejected rather than
// silently creating a rule nothing will ever fire.
func (s *Store) SetNotificationSetting(ctx context.Context, actor User, in NotificationSetting) error {
	if err := validateRecipientList("Always also send To", in.ToRecipients); err != nil {
		return err
	}
	if err := validateRecipientList("Always copy (Cc)", in.CcRecipients); err != nil {
		return err
	}
	if strings.TrimSpace(in.Event) == "" {
		return fmt.Errorf("%w: an event is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE notification_settings SET
		email_enabled=?, to_recipients=?, cc_recipients=?,
		include_requester=?, include_manager=?, include_accounts=?,
		subject_template=?, body_template=? WHERE event=?`,
		boolInt(in.EmailEnabled), strings.TrimSpace(in.ToRecipients), strings.TrimSpace(in.CcRecipients),
		boolInt(in.IncludeRequester), boolInt(in.IncludeManager), boolInt(in.IncludeAccounts),
		in.SubjectTemplate, in.BodyTemplate, in.Event)
	if err != nil {
		return classify(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "notification_setting",
		Summary: "Updated notification rule " + in.Event}); err != nil {
		return err
	}
	return tx.Commit()
}

// UsersWithPermission resolves a resource/action pair to the active users who
// hold it through any role. It is how an event addressed to "the Accounts
// group" finds real people without naming a role anywhere.
func (s *Store) UsersWithPermission(ctx context.Context, resource, action string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at,u.default_approver_id,u.session_version
		FROM users u
		JOIN user_roles ur ON ur.user_id=u.id
		JOIN role_permissions rp ON rp.role_id=ur.role_id
		WHERE u.active=1 AND rp.resource=? AND rp.action=?
		ORDER BY u.name`, resource, action)
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
