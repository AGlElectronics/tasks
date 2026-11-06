-- TASK-22: automations. A rule belongs to a space (optionally scoped to one
-- list) and is stored as data: a trigger, a list of conditions that must all
-- hold, and a list of actions executed in order. Rules run inside the same
-- transaction as the change that triggered them, so a rolled-back change
-- never half-applies a rule. automation_runs is the visible run history.
-- +goose Up
CREATE TABLE automations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    list_id UUID REFERENCES lists (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    trigger JSONB NOT NULL,
    conditions JSONB NOT NULL DEFAULT '[]',
    actions JSONB NOT NULL DEFAULT '[]',
    created_by UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX automations_space_idx ON automations (space_id);

CREATE TABLE automation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id UUID NOT NULL REFERENCES automations (id) ON DELETE CASCADE,
    task_id UUID REFERENCES tasks (id) ON DELETE SET NULL,
    task_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'error')),
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX automation_runs_idx ON automation_runs (automation_id, created_at DESC);

-- Comments posted by an automation carry its name here (author_id is NULL),
-- so the activity feed can say who - or what - wrote them.
ALTER TABLE comments ADD COLUMN via_automation TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE comments DROP COLUMN via_automation;
DROP TABLE automation_runs;
DROP TABLE automations;
