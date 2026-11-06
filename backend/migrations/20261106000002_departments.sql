-- One space per HR department (TASK-25). HR calls POST
-- /api/internal/departments when a department is created or renamed; the
-- unique index makes "exactly one space per department" a database
-- guarantee, not a convention.

-- +goose Up
ALTER TABLE spaces ADD COLUMN department_id UUID;
CREATE UNIQUE INDEX spaces_department_key ON spaces (department_id) WHERE department_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS spaces_department_key;
ALTER TABLE spaces DROP COLUMN IF EXISTS department_id;
