-- Archived spaces (HR department hook with "archived": true, or a space
-- manager). They leave the sidebar and search but keep every task; the
-- space's managers still find them under "Archived spaces".

-- +goose Up
ALTER TABLE spaces ADD COLUMN archived_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE spaces DROP COLUMN IF EXISTS archived_at;
