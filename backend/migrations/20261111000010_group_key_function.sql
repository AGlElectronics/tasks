-- Group grants match with the shared group rule (groupmatch), including
-- OIDC_GROUP_PREFIX and OIDC_GROUP_MATCH=last. tasks_group_key applies
-- groupmatch.Config.Key to a stored subject_key, which is already
-- normalised (trimmed, one leading '/' removed, lower-cased):
--
--   prefix: the normalised prefix without a trailing '/', or '' for none.
--           The prefix group itself becomes '' (matches nothing); names
--           under it lose 'prefix/'; other names stay as they are.
--   mode:   'full' keeps the path; 'last' keeps only the last segment
--           (after removing one trailing '/').
--
-- With prefix '' and mode 'full' it returns subject_key unchanged, so
-- stored grants match exactly as before. Existing subject_key values need
-- no change.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION tasks_group_key(k TEXT, prefix TEXT, mode TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE WHEN mode = 'last'
                THEN regexp_replace(regexp_replace(s.k, '/$', ''), '^.*/', '')
                ELSE s.k END
      FROM (SELECT CASE WHEN prefix = '' THEN k
                        WHEN k = prefix THEN ''
                        WHEN starts_with(k, prefix || '/') THEN substr(k, length(prefix) + 2)
                        ELSE k END AS k) s
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS tasks_group_key(TEXT, TEXT, TEXT);
