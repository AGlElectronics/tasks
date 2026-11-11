-- Module settings (retention, reminders) and email notifications.
--
-- module_settings holds Tasks' own values, edited on Settings by admins.
-- When the module is paired with Den, values Den sends in the integration
-- bundle (module_settings, smtp) win; see internal/tasks/settings.go.
--
-- Email: every Inbox notification starts as email_state 'pending'; the
-- mail worker sends it (or a batch per person) when the person wants that
-- kind by email and has an address, and marks it 'sent', 'skipped' or,
-- after three failed attempts, 'failed'. Rows written before this
-- migration are 'skipped' so nobody gets a flood of old notifications.
--
-- due_reminders remembers which "due soon" reminders went out, one per
-- task, person and due date, so moving the due date reminds again.

-- +goose Up
CREATE TABLE module_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_by UUID REFERENCES users (id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notification_preferences (
    user_id     UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email       BOOLEAN NOT NULL DEFAULT true,
    -- Notification kinds sent by email; the rest stay in the Inbox.
    email_kinds TEXT[] NOT NULL DEFAULT ARRAY['assigned', 'mention', 'due_soon'],
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE notifications ADD COLUMN email_state TEXT NOT NULL DEFAULT 'pending'
    CHECK (email_state IN ('pending', 'sent', 'skipped', 'failed'));
ALTER TABLE notifications ADD COLUMN email_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN email_error TEXT NOT NULL DEFAULT '';
UPDATE notifications SET email_state = 'skipped';
CREATE INDEX notifications_email_pending_idx ON notifications (created_at) WHERE email_state = 'pending';
CREATE INDEX notifications_read_idx ON notifications (read_at) WHERE read_at IS NOT NULL;

CREATE TABLE due_reminders (
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    due_at  DATE NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id, due_at)
);

CREATE INDEX tasks_deleted_at_idx ON tasks (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX automation_runs_created_idx ON automation_runs (created_at);

-- +goose Down
DROP INDEX IF EXISTS automation_runs_created_idx;
DROP INDEX IF EXISTS tasks_deleted_at_idx;
DROP TABLE IF EXISTS due_reminders;
DROP INDEX IF EXISTS notifications_read_idx;
DROP INDEX IF EXISTS notifications_email_pending_idx;
ALTER TABLE notifications DROP COLUMN IF EXISTS email_error;
ALTER TABLE notifications DROP COLUMN IF EXISTS email_attempts;
ALTER TABLE notifications DROP COLUMN IF EXISTS email_state;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS module_settings;
