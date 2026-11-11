package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
)

// Background jobs. The server runs them in-process (one backend replica,
// docs/adr/0001-single-backend-replica.md): retention every hour, due-soon
// reminders every 15 minutes, the email outbox every minute.

// ----------------------------------------------------------------------------
// Retention
// ----------------------------------------------------------------------------

// RetentionResult counts what one retention run removed. ObjectKeys are the
// attachment files of purged tasks, for the caller to delete from S3.
type RetentionResult struct {
	Tasks          int
	Notifications  int
	Audit          int
	AutomationRuns int
	ObjectKeys     []string
}

// retentionBatch bounds one run; the next run continues.
const retentionBatch = 500

// RunRetention applies the retention settings: purges tasks that sat in the
// trash too long (with an audit row by the system), and deletes read
// notifications, audit entries and automation runs past their age.
func (s *Service) RunRetention(ctx context.Context) (RetentionResult, error) {
	var res RetentionResult
	values, _, err := s.effectiveSettings(ctx)
	if err != nil {
		return res, err
	}
	if days := values["retention.trash_days"]; days > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT t.id FROM tasks t
			 WHERE t.deleted_at < now() - make_interval(days => $1)
			   AND (t.parent_id IS NULL OR NOT EXISTS (
			        SELECT 1 FROM tasks p WHERE p.id = t.parent_id AND p.deleted_at IS NOT NULL))
			 ORDER BY t.deleted_at LIMIT $2`, days, retentionBatch)
		if err != nil {
			return res, err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return res, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return res, err
		}
		for _, id := range ids {
			keys, err := s.purge(ctx, nil, id, map[string]any{"reason": "retention", "trash_days": days})
			if err != nil {
				return res, err
			}
			res.Tasks++
			res.ObjectKeys = append(res.ObjectKeys, keys...)
		}
	}
	del := func(days int, sql string, n *int) error {
		if days <= 0 {
			return nil
		}
		tag, err := s.pool.Exec(ctx, sql, days)
		if err == nil {
			*n = int(tag.RowsAffected())
		}
		return err
	}
	if err := del(values["retention.notification_days"],
		`DELETE FROM notifications WHERE read_at < now() - make_interval(days => $1)`, &res.Notifications); err != nil {
		return res, err
	}
	if err := del(values["retention.audit_days"],
		`DELETE FROM audit_log WHERE created_at < now() - make_interval(days => $1)`, &res.Audit); err != nil {
		return res, err
	}
	if err := del(values["retention.automation_run_days"],
		`DELETE FROM automation_runs WHERE created_at < now() - make_interval(days => $1)`, &res.AutomationRuns); err != nil {
		return res, err
	}
	return res, nil
}

// purge permanently deletes a trashed task (and its subtasks) with an audit
// row, and returns the S3 keys of its attachments.
func (s *Service) purge(ctx context.Context, actor *uuid.UUID, id uuid.UUID, extra map[string]any) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var key, title string
	err = tx.QueryRow(ctx, `
		SELECT sp.key || '-' || t.number, t.title FROM tasks t JOIN spaces sp ON sp.id = t.space_id
		 WHERE t.id = $1 AND t.deleted_at IS NOT NULL FOR UPDATE OF t`, id).Scan(&key, &title)
	if err == pgx.ErrNoRows {
		return nil, httperror.NotFound("task")
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT a.s3_key FROM attachments a JOIN tasks t ON t.id = a.task_id WHERE t.id = $1 OR t.parent_id = $1`, id)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	data := map[string]any{"key": key, "title": title}
	for k, v := range extra {
		data[k] = v
	}
	if err := writeAudit(ctx, tx, actor, "task", id, "purged", data); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id); err != nil {
		return nil, err
	}
	return keys, tx.Commit(ctx)
}

// ----------------------------------------------------------------------------
// Due-soon reminders
// ----------------------------------------------------------------------------

// RunDueReminders gives every assignee of an open task due within
// reminders.due_soon_days (today included) one Inbox notification of kind
// due_soon per due date. Dates are the database's CURRENT_DATE.
func (s *Service) RunDueReminders(ctx context.Context) (int, error) {
	days, err := s.Setting(ctx, "reminders.due_soon_days")
	if err != nil || days <= 0 {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `
		SELECT t.id, a.user_id, t.due_at, (t.due_at - CURRENT_DATE)::int
		  FROM tasks t
		  JOIN statuses st ON st.id = t.status_id
		  JOIN spaces sp ON sp.id = t.space_id
		  JOIN task_assignees a ON a.task_id = t.id
		 WHERE t.deleted_at IS NULL AND st.category <> 'done' AND sp.archived_at IS NULL
		   AND t.due_at BETWEEN CURRENT_DATE AND CURRENT_DATE + $1::int
		   AND NOT EXISTS (SELECT 1 FROM due_reminders r WHERE r.task_id = t.id AND r.user_id = a.user_id AND r.due_at = t.due_at)
		 LIMIT 1000`, days)
	if err != nil {
		return 0, err
	}
	type due struct {
		task, user uuid.UUID
		date       time.Time
		inDays     int
	}
	var all []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.task, &d.user, &d.date, &d.inDays); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, d := range all {
		msg := "is due " + d.date.Format("Mon 2 Jan")
		switch d.inDays {
		case 0:
			msg = "is due today"
		case 1:
			msg = "is due tomorrow"
		}
		if err := autoNotify(ctx, tx, d.task, nil, "due_soon", msg, []uuid.UUID{d.user}); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO due_reminders (task_id, user_id, due_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			d.task, d.user, d.date); err != nil {
			return 0, err
		}
	}
	return len(all), tx.Commit(ctx)
}

// ----------------------------------------------------------------------------
// Email
// ----------------------------------------------------------------------------

// NotificationKinds are the Inbox notification kinds; people choose which
// of them also come by email.
var NotificationKinds = []string{"assigned", "mention", "comment", "status", "due_soon", "automation"}

// DefaultEmailKinds are emailed unless the person changes it.
var DefaultEmailKinds = []string{"assigned", "mention", "due_soon"}

// NotificationPrefs is GET/PUT /api/v1/me/notifications.
type NotificationPrefs struct {
	Email      bool     `json:"email"`
	EmailKinds []string `json:"email_kinds"`
	// HasAddress is false when the IdP sent no email address for the person.
	HasAddress bool `json:"has_address"`
}

// NotificationPreferences loads a person's email choices (defaults when
// they never changed them).
func (s *Service) NotificationPreferences(ctx context.Context, userID uuid.UUID) (NotificationPrefs, error) {
	p := NotificationPrefs{Email: true, EmailKinds: append([]string{}, DefaultEmailKinds...)}
	var email *string
	err := s.pool.QueryRow(ctx, `
		SELECT u.email, COALESCE(np.email, true), COALESCE(np.email_kinds, $2)
		  FROM users u LEFT JOIN notification_preferences np ON np.user_id = u.id WHERE u.id = $1`,
		userID, DefaultEmailKinds).Scan(&email, &p.Email, &p.EmailKinds)
	p.HasAddress = email != nil && strings.TrimSpace(*email) != ""
	return p, err
}

// SetNotificationPreferences stores a person's email choices.
func (s *Service) SetNotificationPreferences(ctx context.Context, userID uuid.UUID, in NotificationPrefs) (NotificationPrefs, error) {
	kinds := []string{}
	seen := map[string]bool{}
	for _, k := range in.EmailKinds {
		known := false
		for _, n := range NotificationKinds {
			known = known || n == k
		}
		if !known {
			return NotificationPrefs{}, httperror.Validation("invalid preferences",
				map[string]string{"email_kinds": "unknown kind " + k + " (allowed: " + strings.Join(NotificationKinds, ", ") + ")"})
		}
		if !seen[k] {
			seen[k] = true
			kinds = append(kinds, k)
		}
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, email, email_kinds, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email, email_kinds = EXCLUDED.email_kinds, updated_at = now()`,
		userID, in.Email, kinds); err != nil {
		return NotificationPrefs{}, err
	}
	return s.NotificationPreferences(ctx, userID)
}

// SendFunc delivers email; mail.Send in production, a recorder in tests.
type SendFunc func(ctx context.Context, cfg mail.Config, msgs ...mail.Message) error

// Email outbox tuning.
const (
	// EmailDelay lets a burst of changes collect into one email and lets
	// people who are in the app read the notification there first.
	EmailDelay = 2 * time.Minute
	// emailMaxAge: pending notifications older than this are not emailed
	// any more (for example, SMTP was down or only configured later).
	emailMaxAge  = 24 * time.Hour
	emailBatch   = 500
	emailMaxTry  = 3
	emailPerMail = 20
)

type outgoing struct {
	id                     uuid.UUID
	userID                 uuid.UUID
	email, name            string
	key, title, actor, msg string
	kind                   string
	createdAt              time.Time
}

// RunEmailOutbox emails pending notifications older than delay: one email
// per person with up to 20 notifications. It marks notifications nobody
// should get by email (no address, opted out, kind not chosen, already
// read in the app, too old) as skipped. Without SMTP settings nothing is
// sent and pending notifications age out.
func (s *Service) RunEmailOutbox(ctx context.Context, send SendFunc, delay time.Duration) (int, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE notifications n SET email_state = 'skipped'
		  FROM users u LEFT JOIN notification_preferences np ON np.user_id = u.id
		 WHERE n.email_state = 'pending' AND u.id = n.user_id
		   AND (n.created_at < now() - $1::interval OR n.read_at IS NOT NULL
		        OR COALESCE(btrim(u.email), '') = '' OR NOT COALESCE(np.email, true)
		        OR NOT (n.kind = ANY (COALESCE(np.email_kinds, $2))))`,
		emailMaxAge.String(), DefaultEmailKinds); err != nil {
		return 0, err
	}
	cfg, _, ok, err := s.MailConfig(ctx)
	if err != nil || !ok {
		return 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.user_id, u.email, u.display_name, n.task_key, n.task_title,
		       COALESCE(a.display_name, 'Tasks'), n.message, n.kind, n.created_at
		  FROM notifications n JOIN users u ON u.id = n.user_id LEFT JOIN users a ON a.id = n.actor_id
		 WHERE n.email_state = 'pending' AND n.created_at <= now() - $1::interval
		 ORDER BY n.user_id, n.created_at LIMIT $2`, delay.String(), emailBatch)
	if err != nil {
		return 0, err
	}
	byUser := map[uuid.UUID][]outgoing{}
	var order []uuid.UUID
	for rows.Next() {
		var o outgoing
		if err := rows.Scan(&o.id, &o.userID, &o.email, &o.name, &o.key, &o.title, &o.actor, &o.msg, &o.kind, &o.createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		if _, seen := byUser[o.userID]; !seen {
			order = append(order, o.userID)
		}
		byUser[o.userID] = append(byUser[o.userID], o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	sent := 0
	for _, uid := range order {
		items := byUser[uid]
		for len(items) > 0 {
			n := min(len(items), emailPerMail)
			chunk := items[:n]
			items = items[n:]
			ids := make([]uuid.UUID, len(chunk))
			for i, o := range chunk {
				ids[i] = o.id
			}
			sendCtx, cancel := context.WithTimeout(ctx, time.Minute)
			err := send(sendCtx, cfg, s.renderEmail(chunk))
			cancel()
			if err != nil {
				if _, uerr := s.pool.Exec(ctx, `
					UPDATE notifications SET email_attempts = email_attempts + 1, email_error = left($2, 500),
					       email_state = CASE WHEN email_attempts + 1 >= $3 THEN 'failed' ELSE 'pending' END
					 WHERE id = ANY($1)`, ids, err.Error(), emailMaxTry); uerr != nil {
					return sent, uerr
				}
				continue
			}
			if _, err := s.pool.Exec(ctx, `UPDATE notifications SET email_state = 'sent', email_error = '' WHERE id = ANY($1)`, ids); err != nil {
				return sent, err
			}
			sent++
		}
	}
	return sent, nil
}

func (s *Service) renderEmail(items []outgoing) mail.Message {
	sort.SliceStable(items, func(i, j int) bool { return items[i].createdAt.Before(items[j].createdAt) })
	line := func(o outgoing) string {
		who := o.actor
		if o.kind == "due_soon" {
			who = ""
		}
		return strings.TrimSpace(fmt.Sprintf("%s %s: %s %s", o.key, o.title, who, o.msg))
	}
	link := func(o outgoing) string {
		if s.publicURL == "" || o.key == "" {
			return ""
		}
		return s.publicURL + "/t/" + o.key
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Hello %s,\n\n", items[0].name)
	subject := ""
	if len(items) == 1 {
		o := items[0]
		subject = "[Tasks] " + line(o)
		b.WriteString(line(o) + "\n")
		if l := link(o); l != "" {
			b.WriteString(l + "\n")
		}
	} else {
		subject = fmt.Sprintf("[Tasks] %d updates on your tasks", len(items))
		for _, o := range items {
			b.WriteString("- " + line(o) + "\n")
			if l := link(o); l != "" {
				b.WriteString("  " + l + "\n")
			}
		}
	}
	if len(subject) > 200 {
		subject = subject[:200] + "…"
	}
	b.WriteString("\nYou get this email because of your notification settings in Tasks (Inbox → Email settings).\n")
	return mail.Message{To: items[0].email, Subject: subject, Body: b.String()}
}

// SendTestEmail sends one email to the given address with the current SMTP
// settings, so an admin can check them.
func (s *Service) SendTestEmail(ctx context.Context, send SendFunc, to string) error {
	cfg, source, ok, err := s.MailConfig(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return httperror.Conflict("email is not configured: set SMTP_HOST and SMTP_FROM, or pair with a Den that sends SMTP settings")
	}
	if strings.TrimSpace(to) == "" {
		return httperror.Conflict("your account has no email address")
	}
	err = send(ctx, cfg, mail.Message{
		To:      to,
		Subject: "[Tasks] Test email",
		Body:    "This is a test email from Tasks. SMTP settings come from: " + source + ".\n",
	})
	if err != nil {
		return httperror.New(httperror.CodeUnavailable, "the mail server refused the test email: "+err.Error())
	}
	return nil
}
