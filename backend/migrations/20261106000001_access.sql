-- Access control (TASK-1, TASK-27) and the delete audit trail (TASK-29).
--
-- Access model:
--   * spaces.everyone_level: NULL = closed; otherwise every signed-in user
--     gets that level. Existing spaces are backfilled to 'edit' so nobody
--     loses access at the cutover; NEW spaces start closed (TASK-24).
--   * access_grants: a user or an IdP group gets view/edit/manage on a
--     whole space OR on a single list (cross-team sharing without opening
--     the whole space).
--   * lists.private: visible only through a list grant or to space managers.
--   * Assignment to a task (and later watching it) opens that one task.
--
-- Audit trail:
--   * task_events survive a task delete (FK ON DELETE SET NULL + snapshot).
--   * audit_log records task/list/space/comment deletes with a snapshot.

-- +goose Up
CREATE TABLE access_grants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id     UUID REFERENCES spaces(id) ON DELETE CASCADE,
    list_id      UUID REFERENCES lists(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'group')),
    -- users: the users.id uuid as text; groups: the IdP group name verbatim.
    subject      TEXT NOT NULL,
    level        TEXT NOT NULL CHECK (level IN ('view', 'edit', 'manage')),
    created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((space_id IS NULL) <> (list_id IS NULL))
);

CREATE UNIQUE INDEX access_grants_space_subject_key
    ON access_grants (space_id, subject_type, subject) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX access_grants_list_subject_key
    ON access_grants (list_id, subject_type, subject) WHERE list_id IS NOT NULL;

ALTER TABLE spaces ADD COLUMN everyone_level TEXT
    CHECK (everyone_level IN ('view', 'edit', 'manage'));

-- Pre-grants spaces were open to every signed-in user; keep that so the
-- cutover changes nothing for existing spaces.
UPDATE spaces SET everyone_level = 'edit';

ALTER TABLE lists ADD COLUMN private BOOLEAN NOT NULL DEFAULT false;

-- Keep per-task activity when the task goes away (TASK-29). The snapshot
-- columns are filled by every event write so a detached event still says
-- which task it was about.
ALTER TABLE task_events ALTER COLUMN task_id DROP NOT NULL;
ALTER TABLE task_events DROP CONSTRAINT task_events_task_id_fkey;
ALTER TABLE task_events ADD CONSTRAINT task_events_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE SET NULL;
ALTER TABLE task_events ADD COLUMN task_key TEXT;
ALTER TABLE task_events ADD COLUMN task_title TEXT;

CREATE TABLE audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    entity     TEXT NOT NULL CHECK (entity IN ('task', 'list', 'space', 'comment')),
    entity_id  UUID NOT NULL,
    action     TEXT NOT NULL DEFAULT 'deleted',
    -- Snapshot of whatever identifies the entity to a human: key, title,
    -- name, counts. Shape varies per entity.
    data       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id);
CREATE INDEX audit_log_created_idx ON audit_log (created_at);

-- +goose Down
DROP TABLE IF EXISTS audit_log;
ALTER TABLE task_events DROP COLUMN IF EXISTS task_title;
ALTER TABLE task_events DROP COLUMN IF EXISTS task_key;
ALTER TABLE task_events DROP CONSTRAINT task_events_task_id_fkey;
DELETE FROM task_events WHERE task_id IS NULL;
ALTER TABLE task_events ALTER COLUMN task_id SET NOT NULL;
ALTER TABLE task_events ADD CONSTRAINT task_events_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE lists DROP COLUMN IF EXISTS private;
ALTER TABLE spaces DROP COLUMN IF EXISTS everyone_level;
DROP TABLE IF EXISTS access_grants;
