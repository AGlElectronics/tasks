-- Group grants match the shared group rule: the name is trimmed, one
-- leading '/' (Keycloak's full-path form) is removed and it is lower-cased;
-- the whole path must match. subject keeps what the admin typed (shown in
-- the UI); subject_key is the normalised form used for matching and for
-- the one-grant-per-subject unique indexes. User grants use the user id in
-- both columns.

-- +goose Up
ALTER TABLE access_grants ADD COLUMN subject_key TEXT;

UPDATE access_grants
   SET subject_key = CASE WHEN subject_type = 'group'
                          THEN lower(regexp_replace(btrim(subject, E' \t\r\n'), '^/', ''))
                          ELSE subject END;

-- Grants that only differed in case or a leading '/' now name the same
-- group: keep the strongest level (the oldest row on a tie).
DELETE FROM access_grants g
 USING access_grants o
 WHERE g.id <> o.id
   AND g.subject_type = o.subject_type
   AND g.subject_key = o.subject_key
   AND g.space_id IS NOT DISTINCT FROM o.space_id
   AND g.list_id IS NOT DISTINCT FROM o.list_id
   AND (CASE g.level WHEN 'view' THEN 1 WHEN 'edit' THEN 2 ELSE 3 END,
        o.created_at, o.id::text)
     < (CASE o.level WHEN 'view' THEN 1 WHEN 'edit' THEN 2 ELSE 3 END,
        g.created_at, g.id::text);

ALTER TABLE access_grants ALTER COLUMN subject_key SET NOT NULL;

DROP INDEX access_grants_space_subject_key;
DROP INDEX access_grants_list_subject_key;
CREATE UNIQUE INDEX access_grants_space_subject_key
    ON access_grants (space_id, subject_type, subject_key) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX access_grants_list_subject_key
    ON access_grants (list_id, subject_type, subject_key) WHERE list_id IS NOT NULL;
CREATE INDEX access_grants_subject_idx ON access_grants (subject_type, subject_key);

-- +goose Down
DROP INDEX IF EXISTS access_grants_subject_idx;
DROP INDEX access_grants_space_subject_key;
DROP INDEX access_grants_list_subject_key;
CREATE UNIQUE INDEX access_grants_space_subject_key
    ON access_grants (space_id, subject_type, subject) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX access_grants_list_subject_key
    ON access_grants (list_id, subject_type, subject) WHERE list_id IS NOT NULL;
ALTER TABLE access_grants DROP COLUMN subject_key;
