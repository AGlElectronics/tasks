-- Tasks — initial schema.
--
-- Scope: the MVP slice — organizations, users keyed by the OIDC
-- (issuer, subject) pair, spaces with their status sets, lists, tasks with
-- subtasks and assignees, comments, the task activity log, and the Den
-- pairing row. Folders, custom fields, views and attachments are later
-- migrations (docs/plan.md).

-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

CREATE TABLE organizations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (slug = lower(slug) AND slug ~ '^[a-z0-9][a-z0-9-]*[a-z0-9]$')
);

-- Roles are NOT stored here: they come from IdP groups via the access
-- config on every request.
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issuer        TEXT NOT NULL,
    subject       TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    email         TEXT,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

CREATE INDEX users_email_lower_idx ON users (lower(email));

-- ----------------------------------------------------------------------------
-- SPACES / STATUSES / LISTS
-- ----------------------------------------------------------------------------
-- A space usually maps to a department or a big project. Its key prefixes
-- task IDs (SEC-142). Statuses belong to the space; every list in it uses
-- the same set in this slice.

CREATE TABLE spaces (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL,
    key             TEXT NOT NULL UNIQUE CHECK (key ~ '^[A-Z][A-Z0-9]{1,5}$'),
    color           TEXT NOT NULL DEFAULT '#6366f1',
    description     TEXT NOT NULL DEFAULT '',
    next_number     INTEGER NOT NULL DEFAULT 1,
    position        INTEGER NOT NULL DEFAULT 0,
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE statuses (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    color    TEXT NOT NULL DEFAULT '#9ca3af',
    category TEXT NOT NULL CHECK (category IN ('not_started', 'active', 'done')),
    position INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX statuses_space_idx ON statuses (space_id, position);

CREATE TABLE lists (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ
);

CREATE INDEX lists_space_idx ON lists (space_id, position);

-- ----------------------------------------------------------------------------
-- TASKS
-- ----------------------------------------------------------------------------
-- position is a float so a drag writes one row: the new position is the
-- midpoint of its neighbours. Subtasks live in their parent's list.

CREATE TABLE tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id     UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    list_id      UUID NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    parent_id    UUID REFERENCES tasks(id) ON DELETE CASCADE,
    number       INTEGER NOT NULL,
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    status_id    UUID NOT NULL REFERENCES statuses(id) ON DELETE RESTRICT,
    priority     TEXT CHECK (priority IN ('urgent', 'high', 'normal', 'low')),
    start_at     DATE,
    due_at       DATE,
    position     DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    completed_at TIMESTAMPTZ,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (space_id, number)
);

CREATE INDEX tasks_list_idx ON tasks (list_id, position);
CREATE INDEX tasks_parent_idx ON tasks (parent_id);
CREATE INDEX tasks_status_idx ON tasks (status_id);

CREATE TABLE task_assignees (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, user_id)
);

CREATE INDEX task_assignees_user_idx ON task_assignees (user_id);

CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at  TIMESTAMPTZ
);

CREATE INDEX comments_task_idx ON comments (task_id, created_at);

-- Activity log shown on every task ("Alice changed status To do → Done").
CREATE TABLE task_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL,
    data       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX task_events_task_idx ON task_events (task_id, created_at);

-- ----------------------------------------------------------------------------
-- DEN PAIRING
-- ----------------------------------------------------------------------------

CREATE TABLE den_link (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    den_base_url  TEXT NOT NULL,
    credential    TEXT NOT NULL,
    module_id     TEXT NOT NULL DEFAULT '',
    paired_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    synced_at     TIMESTAMPTZ,
    last_error    TEXT NOT NULL DEFAULT '',
    bundle        JSONB
);

INSERT INTO organizations (id, slug, display_name)
VALUES ('00000000-0000-0000-0000-000000000010', 'demo-org', 'Demo Organization')
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS den_link;
DROP TABLE IF EXISTS task_events;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS task_assignees;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS lists;
DROP TABLE IF EXISTS statuses;
DROP TABLE IF EXISTS spaces;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
