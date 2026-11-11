-- The shared denlink package records when Den rejects the module
-- credential (revoked_at); an admin then has to pair again. Same statement
-- as denlink.SchemaUpgrade.

-- +goose Up
ALTER TABLE den_link ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE den_link DROP COLUMN IF EXISTS revoked_at;
