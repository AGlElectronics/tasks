package http

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// mailbox records what would have been sent.
type mailbox struct {
	mu   sync.Mutex
	msgs []mail.Message
	fail error
}

func (m *mailbox) send(_ context.Context, cfg mail.Config, msgs ...mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if !cfg.Enabled() {
		return mail.ErrNotConfigured
	}
	m.msgs = append(m.msgs, msgs...)
	return nil
}

func (m *mailbox) take() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.msgs
	m.msgs = nil
	return out
}

func TestModuleSettings(t *testing.T) {
	box := &mailbox{}
	h := newHarnessWith(t, func(c *config.Config) {
		c.SMTPHost, c.SMTPFrom, c.SMTPSecurity = "smtp.example.org", "tasks@example.org", "starttls"
	})
	h.rebuild(func(d *Deps) { d.SendMail = box.send })
	admin := as{user: "admin", groups: "tasks-admin"}

	h.must(as{user: "bob"}, "GET", "/api/v1/settings", nil, 403)
	v := h.must(admin, "GET", "/api/v1/settings", nil, 200)
	ret := v["settings"].(map[string]any)["retention"].(map[string]any)
	if ret["trash_days"] != float64(30) || v["sources"].(map[string]any)["retention.trash_days"] != "default" {
		t.Fatalf("defaults: %v", v)
	}
	email := v["email"].(map[string]any)
	if email["configured"] != true || email["source"] != "tasks" || email["host"] != "smtp.example.org" || email["password"] != nil {
		t.Fatalf("email status: %v", email)
	}

	v = h.must(admin, "PUT", "/api/v1/settings", map[string]any{"retention": map[string]any{"trash_days": 7}}, 200)
	if v["settings"].(map[string]any)["retention"].(map[string]any)["trash_days"] != float64(7) ||
		v["sources"].(map[string]any)["retention.trash_days"] != "tasks" {
		t.Fatalf("own value: %v", v)
	}
	h.must(admin, "PUT", "/api/v1/settings", map[string]any{"retention": map[string]any{"nope": 1}}, 422)
	h.must(admin, "PUT", "/api/v1/settings", map[string]any{"reminders": map[string]any{"due_soon_days": 99}}, 422)
	audit := h.must(admin, "GET", "/api/v1/audit?entity=settings", nil, 200)["entries"].([]any)
	if len(audit) != 1 || audit[0].(map[string]any)["data"].(map[string]any)["retention.trash_days"] != float64(7) {
		t.Fatalf("settings audit: %v", audit)
	}

	// Test email goes to the admin's own address with the own SMTP.
	out := h.must(admin, "POST", "/api/v1/settings/email/test", map[string]any{}, 200)
	if out["sent_to"] != "admin@tasks.local" {
		t.Fatalf("test email: %v", out)
	}
	if msgs := box.take(); len(msgs) != 1 || msgs[0].To != "admin@tasks.local" {
		t.Fatalf("sent: %v", msgs)
	}
	box.fail = errors.New("550 relay denied")
	h.must(admin, "POST", "/api/v1/settings/email/test", map[string]any{}, 503)
	box.fail = nil

	// Paired with a Den that sends module settings and SMTP: Den wins and
	// its values cannot be changed here.
	if _, err := h.pool.Exec(context.Background(), `
		INSERT INTO den_link (den_base_url, credential, bundle) VALUES ('http://den', 'x', $1)`,
		`{"brand":{},"settings":{"data":{}},"module_settings":{"data":{
		    "retention":{"trash_days":3,"audit_days":99999},
		    "smtp":{"host":"mail.den.example","port":465,"from":"den@example.org","security":"tls"}}}}`); err != nil {
		t.Fatal(err)
	}
	den := denlink.New(h.pool, denlink.Config{Slug: "tasks", AppEnv: "development"}, zerolog.Nop())
	if err := den.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.rebuild(func(d *Deps) { d.Den = den })
	v = h.must(admin, "GET", "/api/v1/settings", nil, 200)
	src := v["sources"].(map[string]any)
	if v["settings"].(map[string]any)["retention"].(map[string]any)["trash_days"] != float64(3) || src["retention.trash_days"] != "den" {
		t.Fatalf("Den should win: %v", v)
	}
	if src["retention.audit_days"] != "default" {
		t.Fatalf("an out-of-range Den value is ignored, got %v", src["retention.audit_days"])
	}
	email = v["email"].(map[string]any)
	if email["source"] != "den" || email["host"] != "mail.den.example" || email["port"] != float64(465) {
		t.Fatalf("Den SMTP: %v", email)
	}
	rec, body := h.do(admin, "PUT", "/api/v1/settings", map[string]any{"retention": map[string]any{"trash_days": 9}})
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "managed by Den") {
		t.Fatalf("Den-managed setting must be refused, got %d %v", rec.Code, body)
	}
}

func TestRetentionRemindersAndEmail(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobMe := h.must(bob, "GET", "/api/v1/me", nil, 200)
	bobID := str(bobMe, "id")

	svc := tasks.NewService(h.pool)
	svc.SetEnvSMTP(mail.Config{Host: "smtp.example.org", From: "tasks@example.org"})
	svc.SetPublicURL("https://tasks.example.org/")
	box := &mailbox{}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Jobs", "key": "JOB"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")

	// --- Email: assignment is emailed; the link uses TASKS_PUBLIC_URL.
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "Radios", "assignee_ids": []string{bobID}}, 201)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{"assignee_ids": []string{}, "version": task["version"]}, 200)
	task = h.must(alice, "GET", "/api/v1/tasks/"+str(task, "id"), nil, 200)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{"assignee_ids": []string{bobID}, "version": task["version"]}, 200)
	n, err := svc.RunEmailOutbox(ctx, box.send, 0)
	if err != nil || n != 1 {
		t.Fatalf("outbox: %d %v", n, err)
	}
	msgs := box.take()
	if len(msgs) != 1 || msgs[0].To != "bob@tasks.local" || !strings.Contains(msgs[0].Body, "https://tasks.example.org/t/JOB-1") ||
		!strings.Contains(msgs[0].Subject, "JOB-1 Radios") {
		t.Fatalf("email: %+v", msgs)
	}
	if n, _ := svc.RunEmailOutbox(ctx, box.send, 0); n != 0 {
		t.Fatal("a sent notification is not sent again")
	}

	// Comments are not emailed by default; opting out stops assignment mails.
	h.must(alice, "POST", "/api/v1/tasks/"+str(task, "id")+"/comments", map[string]any{"body": "hi"}, 201)
	prefs := h.must(bob, "GET", "/api/v1/me/notifications", nil, 200)
	if prefs["email"] != true || prefs["has_address"] != true || len(prefs["email_kinds"].([]any)) != 3 {
		t.Fatalf("default prefs: %v", prefs)
	}
	h.must(bob, "PUT", "/api/v1/me/notifications", map[string]any{"email": true, "email_kinds": []string{"nope"}}, 422)
	h.must(bob, "PUT", "/api/v1/me/notifications", map[string]any{"email": false, "email_kinds": []string{"assigned"}}, 200)
	t2 := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "Second"}, 201)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(t2, "id"), map[string]any{"assignee_ids": []string{bobID}, "version": t2["version"]}, 200)
	if n, _ := svc.RunEmailOutbox(ctx, box.send, 0); n != 0 || len(box.take()) != 0 {
		t.Fatal("opted out: nothing is emailed")
	}
	var pending int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE email_state = 'pending'`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("skipped notifications must not stay pending, %d left", pending)
	}

	// Failures retry, then give up.
	h.must(bob, "PUT", "/api/v1/me/notifications", map[string]any{"email": true, "email_kinds": []string{"assigned", "mention"}}, 200)
	t3 := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "Third"}, 201)
	t3 = h.must(alice, "PATCH", "/api/v1/tasks/"+str(t3, "id"), map[string]any{"assignee_ids": []string{bobID}, "version": t3["version"]}, 200)
	box.fail = errors.New("421 try later")
	for i := 0; i < 3; i++ {
		_, _ = svc.RunEmailOutbox(ctx, box.send, 0)
	}
	var state, emailErr string
	_ = h.pool.QueryRow(ctx, `SELECT email_state, email_error FROM notifications WHERE task_key = 'JOB-3'`).Scan(&state, &emailErr)
	if state != "failed" || !strings.Contains(emailErr, "421") {
		t.Fatalf("after 3 failures: %s %q", state, emailErr)
	}
	box.fail = nil

	// --- Due-soon reminders: once per task, person and due date.
	t3 = h.must(alice, "PATCH", "/api/v1/tasks/"+str(t3, "id"), map[string]any{"due_at": todayPlus(h, 1), "version": t3["version"]}, 200)
	if n, err := svc.RunDueReminders(ctx); err != nil || n != 1 {
		t.Fatalf("reminders: %d %v", n, err)
	}
	if n, _ := svc.RunDueReminders(ctx); n != 0 {
		t.Fatal("a reminder is sent once per due date")
	}
	inbox := h.must(bob, "GET", "/api/v1/inbox", nil, 200)["notifications"].([]any)
	if first := inbox[0].(map[string]any); first["kind"] != "due_soon" || first["message"] != "is due tomorrow" || first["actor_name"] != "Someone" {
		t.Fatalf("reminder: %v", first)
	}
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(t3, "id"), map[string]any{"due_at": todayPlus(h, 0), "version": t3["version"]}, 200)
	if n, _ := svc.RunDueReminders(ctx); n != 1 {
		t.Fatal("a new due date reminds again")
	}
	h.must(bob, "PUT", "/api/v1/me/notifications", map[string]any{"email": true, "email_kinds": []string{"assigned", "mention", "due_soon"}}, 200)
	if n, _ := svc.RunEmailOutbox(ctx, box.send, 0); n != 1 {
		t.Fatal("both reminders go out in one email")
	}
	if m := box.take(); len(m) != 1 || !strings.Contains(m[0].Body, "is due today") || !strings.Contains(m[0].Subject, "2 updates") {
		t.Fatalf("reminder email: %+v", m)
	}

	// --- Retention: old trash is purged by the system, old read
	// notifications go, the rest stays.
	h.must(alice, "DELETE", "/api/v1/tasks/"+str(t2, "id"), nil, 204)
	h.must(alice, "DELETE", "/api/v1/tasks/"+str(t3, "id"), nil, 204)
	if _, err := h.pool.Exec(ctx, `UPDATE tasks SET deleted_at = now() - interval '40 days' WHERE id = $1`, str(t2, "id")); err != nil {
		t.Fatal(err)
	}
	h.must(bob, "POST", "/api/v1/inbox/read", map[string]any{}, 204)
	if _, err := h.pool.Exec(ctx, `UPDATE notifications SET read_at = now() - interval '100 days' WHERE task_key = 'JOB-1'`); err != nil {
		t.Fatal(err)
	}
	res, err := svc.RunRetention(ctx)
	if err != nil || res.Tasks != 1 || res.Notifications < 1 {
		t.Fatalf("retention: %+v %v", res, err)
	}
	var purgedBySystem int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'purged' AND actor_id IS NULL AND data->>'reason' = 'retention'`).Scan(&purgedBySystem)
	if purgedBySystem != 1 {
		t.Fatalf("purge audit: %d", purgedBySystem)
	}
	trash := h.must(alice, "GET", "/api/v1/spaces/"+str(sp, "id")+"/trash", nil, 200)["tasks"].([]any)
	if len(trash) != 1 || str(trash[0].(map[string]any), "key") != "JOB-3" {
		t.Fatalf("the recently trashed task stays: %v", trash)
	}
}

// todayPlus is the database's CURRENT_DATE plus n days, as YYYY-MM-DD.
func todayPlus(h *harness, n int) string {
	var d string
	if err := h.pool.QueryRow(context.Background(), `SELECT to_char(CURRENT_DATE + $1::int, 'YYYY-MM-DD')`, n).Scan(&d); err != nil {
		h.t.Fatal(err)
	}
	return d
}
