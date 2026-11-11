-- Dependencies and recurring tasks.
--
-- task_dependencies: task_id is blocked by depends_on (finish depends_on
-- first). Any two tasks can be linked, also across spaces; cycles are
-- refused by the backend.
--
-- tasks.recurrence: {"freq": "daily"|"weekly"|"monthly"|"yearly",
-- "interval": 1..365}. When a recurring task is completed, the next
-- occurrence is created with its dates moved on, and the rule moves to the
-- new task.

-- +goose Up
CREATE TABLE task_dependencies (
    task_id    UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    depends_on UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    created_by UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, depends_on),
    CHECK (task_id <> depends_on)
);
CREATE INDEX task_dependencies_depends_on_idx ON task_dependencies (depends_on);

ALTER TABLE tasks ADD COLUMN recurrence JSONB;

CREATE TRIGGER rt_dependencies AFTER INSERT OR DELETE ON task_dependencies FOR EACH ROW EXECUTE FUNCTION rt_task_child();

-- +goose Down
DROP TRIGGER IF EXISTS rt_dependencies ON task_dependencies;
ALTER TABLE tasks DROP COLUMN IF EXISTS recurrence;
DROP TABLE IF EXISTS task_dependencies;
