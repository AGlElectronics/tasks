-- Phase 4 features: tags (TASK-4), checklists (TASK-6), watchers and the
-- Inbox (TASK-5), attachments (TASK-7), folders (TASK-9), task-id aliases
-- for cross-space moves (TASK-10/45), favourites and trash (TASK-11/48).
-- Everything is additive, so the pre-cutover binary keeps running against
-- the migrated database.

-- +goose Up

-- Tags are per space, so a space's tag list stays small and meaningful.
CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL DEFAULT '#6366f1',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX tags_space_name_key ON tags (space_id, lower(name));

CREATE TABLE task_tags (
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);
CREATE INDEX task_tags_tag_idx ON task_tags (tag_id);

CREATE TABLE checklist_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    done BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX checklist_items_task_idx ON checklist_items (task_id);

CREATE TABLE task_watchers (
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, user_id)
);

-- The Inbox. task_id is SET NULL on delete with a key/title snapshot, the
-- same pattern as task_events.
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    task_id UUID REFERENCES tasks (id) ON DELETE SET NULL,
    task_key TEXT NOT NULL DEFAULT '',
    task_title TEXT NOT NULL DEFAULT '',
    actor_id UUID REFERENCES users (id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);

CREATE TABLE attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes BIGINT NOT NULL,
    s3_key TEXT NOT NULL,
    uploaded_by UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX attachments_task_idx ON attachments (task_id);

CREATE TABLE folders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE lists ADD COLUMN folder_id UUID REFERENCES folders (id) ON DELETE SET NULL;

-- Old KEY-N references keep resolving after a task moves to another space.
CREATE TABLE task_ids (
    key TEXT PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE
);
CREATE INDEX task_ids_task_idx ON task_ids (task_id);

CREATE TABLE favorites (
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    space_id UUID REFERENCES spaces (id) ON DELETE CASCADE,
    list_id UUID REFERENCES lists (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((space_id IS NULL) <> (list_id IS NULL))
);
CREATE UNIQUE INDEX favorites_space_key ON favorites (user_id, space_id) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX favorites_list_key ON favorites (user_id, list_id) WHERE list_id IS NOT NULL;

-- Attachment deletes are audited like the other destructive actions.
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_check
    CHECK (entity IN ('task', 'list', 'space', 'comment', 'attachment'));

-- Trash: deleting a task parks it here; it disappears from every view but
-- can be restored until it is purged.
ALTER TABLE tasks ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN deleted_by UUID REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX tasks_trash_idx ON tasks (space_id) WHERE deleted_at IS NOT NULL;

-- +goose Down
ALTER TABLE tasks DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE tasks DROP COLUMN IF EXISTS deleted_at;
DROP TABLE IF EXISTS favorites;
DROP TABLE IF EXISTS task_ids;
ALTER TABLE lists DROP COLUMN IF EXISTS folder_id;
DROP TABLE IF EXISTS folders;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS task_watchers;
DROP TABLE IF EXISTS checklist_items;
DROP TABLE IF EXISTS task_tags;
DROP TABLE IF EXISTS tags;
