-- The audit log also records access changes (grants given and revoked),
-- module settings changes and HR's department hook, and it is readable by
-- admins through GET /api/v1/audit (newest first, keyset paged).

-- +goose Up
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_check
    CHECK (entity IN ('task', 'list', 'space', 'comment', 'attachment', 'grant', 'settings'));
-- Purges used to be action 'deleted' with data.purged = true.
UPDATE audit_log SET action = 'purged' WHERE entity = 'task' AND (data->>'purged')::boolean IS TRUE;
CREATE INDEX audit_log_feed_idx ON audit_log (created_at DESC, id DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_log_actor_idx;
DROP INDEX IF EXISTS audit_log_feed_idx;
DELETE FROM audit_log WHERE entity IN ('grant', 'settings');
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_check
    CHECK (entity IN ('task', 'list', 'space', 'comment', 'attachment'));
