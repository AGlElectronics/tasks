-- Custom fields. A space defines its fields (text, number, date, select,
-- checkbox, url); every task in the space can hold one value per field.
-- Values are JSON so each type keeps its natural form (string, number,
-- boolean, "YYYY-MM-DD", or the id of a select option). Moving a task to
-- another space drops its values, like tags, because fields belong to the
-- space.

-- +goose Up
CREATE TABLE custom_fields (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id   UUID NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('text', 'number', 'date', 'select', 'checkbox', 'url')),
    -- select: [{"id": "<uuid>", "name": "Ordered", "color": "#22c55e"}]
    options    JSONB NOT NULL DEFAULT '[]',
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX custom_fields_space_name_key ON custom_fields (space_id, lower(name));

CREATE TABLE task_field_values (
    task_id  UUID NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    field_id UUID NOT NULL REFERENCES custom_fields (id) ON DELETE CASCADE,
    value    JSONB NOT NULL,
    PRIMARY KEY (task_id, field_id)
);
CREATE INDEX task_field_values_field_idx ON task_field_values (field_id);

-- Deleting a field deletes every value in it: audited.
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_check
    CHECK (entity IN ('task', 'list', 'space', 'comment', 'attachment', 'grant', 'settings', 'field'));

CREATE TRIGGER rt_custom_fields AFTER INSERT OR UPDATE OR DELETE ON custom_fields FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_field_values AFTER INSERT OR UPDATE OR DELETE ON task_field_values FOR EACH ROW EXECUTE FUNCTION rt_task_child();

-- +goose Down
DROP TRIGGER IF EXISTS rt_field_values ON task_field_values;
DROP TRIGGER IF EXISTS rt_custom_fields ON custom_fields;
DELETE FROM audit_log WHERE entity = 'field';
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_check
    CHECK (entity IN ('task', 'list', 'space', 'comment', 'attachment', 'grant', 'settings'));
DROP TABLE IF EXISTS task_field_values;
DROP TABLE IF EXISTS custom_fields;
