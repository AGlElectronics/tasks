-- Time tracking: people log time spent on a task (minutes, on a day, with
-- an optional note), and a task can carry an estimate. The workload view
-- sums estimates per assignee.

-- +goose Up
ALTER TABLE tasks ADD COLUMN estimate_minutes INT CHECK (estimate_minutes >= 0);

CREATE TABLE time_entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
    minutes    INT NOT NULL CHECK (minutes > 0 AND minutes <= 1440),
    spent_on   DATE NOT NULL DEFAULT CURRENT_DATE,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX time_entries_task_idx ON time_entries (task_id, spent_on);
CREATE INDEX time_entries_user_idx ON time_entries (user_id, spent_on);

CREATE TRIGGER rt_time_entries AFTER INSERT OR UPDATE OR DELETE ON time_entries FOR EACH ROW EXECUTE FUNCTION rt_task_child();

-- +goose Down
DROP TRIGGER IF EXISTS rt_time_entries ON time_entries;
DROP TABLE IF EXISTS time_entries;
ALTER TABLE tasks DROP COLUMN IF EXISTS estimate_minutes;
