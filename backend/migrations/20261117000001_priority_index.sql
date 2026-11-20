-- Labels and urgency (Tasks 1.1): the list, My Work and Inbox filters and the
-- urgency sort read tasks.priority. Labels (tags) already have
-- task_tags_tag_idx and the tags_space_name_key name index.
--
-- Partial: most tasks have no priority, and trashed tasks never match.

-- +goose Up
CREATE INDEX IF NOT EXISTS tasks_priority_idx ON tasks (priority) WHERE priority IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS tasks_priority_idx;
