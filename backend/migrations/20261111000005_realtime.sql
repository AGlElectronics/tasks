-- Real-time updates (SSE). Triggers announce every change on the channel
-- tasks_changes with a small JSON payload that names WHAT changed, never
-- the content:
--   {"k":"list","l":"<list id>","s":"<space id>"}  tasks and their parts
--   {"k":"space","s":"<space id>"}                  structure and sharing
--   {"k":"inbox","u":"<user id>"}                   a person's notifications
-- The backend LISTENs, checks per connection who may know about the list or
-- space, and tells browsers which queries to refetch. PostgreSQL drops
-- identical payloads within one transaction, so a bulk change sends one
-- message per list.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION rt_notify(payload jsonb) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('tasks_changes', payload::text);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION rt_tasks() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM rt_notify(jsonb_build_object('k', 'list', 'l', OLD.list_id, 's', OLD.space_id));
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM rt_notify(jsonb_build_object('k', 'list', 'l', NEW.list_id, 's', NEW.space_id));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd

-- Tables with a task_id: announce the task's list.
-- +goose StatementBegin
CREATE FUNCTION rt_task_child() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    tid uuid;
    lid uuid;
    sid uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN
        tid := (to_jsonb(OLD)->>'task_id')::uuid;
    ELSE
        tid := (to_jsonb(NEW)->>'task_id')::uuid;
    END IF;
    SELECT list_id, space_id INTO lid, sid FROM tasks WHERE id = tid;
    IF lid IS NOT NULL THEN
        PERFORM rt_notify(jsonb_build_object('k', 'list', 'l', lid, 's', sid));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd

-- Structure and sharing: announce the space. TG_ARGV[0] names the column
-- holding the space id ('id' for spaces itself); rows that only carry a
-- list_id (list grants) resolve the space through lists.
-- +goose StatementBegin
CREATE FUNCTION rt_space() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    r jsonb;
    sid uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN r := to_jsonb(OLD); ELSE r := to_jsonb(NEW); END IF;
    sid := (r->>TG_ARGV[0])::uuid;
    IF sid IS NULL AND r ? 'list_id' THEN
        SELECT space_id INTO sid FROM lists WHERE id = (r->>'list_id')::uuid;
    END IF;
    IF sid IS NOT NULL THEN
        PERFORM rt_notify(jsonb_build_object('k', 'space', 's', sid));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION rt_inbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM rt_notify(jsonb_build_object('k', 'inbox', 'u', OLD.user_id));
    ELSE
        PERFORM rt_notify(jsonb_build_object('k', 'inbox', 'u', NEW.user_id));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER rt_tasks AFTER INSERT OR UPDATE OR DELETE ON tasks FOR EACH ROW EXECUTE FUNCTION rt_tasks();
CREATE TRIGGER rt_comments AFTER INSERT OR UPDATE OR DELETE ON comments FOR EACH ROW EXECUTE FUNCTION rt_task_child();
CREATE TRIGGER rt_checklist AFTER INSERT OR UPDATE OR DELETE ON checklist_items FOR EACH ROW EXECUTE FUNCTION rt_task_child();
CREATE TRIGGER rt_assignees AFTER INSERT OR DELETE ON task_assignees FOR EACH ROW EXECUTE FUNCTION rt_task_child();
CREATE TRIGGER rt_task_tags AFTER INSERT OR DELETE ON task_tags FOR EACH ROW EXECUTE FUNCTION rt_task_child();
CREATE TRIGGER rt_watchers AFTER INSERT OR DELETE ON task_watchers FOR EACH ROW EXECUTE FUNCTION rt_task_child();
CREATE TRIGGER rt_attachments AFTER INSERT OR DELETE ON attachments FOR EACH ROW EXECUTE FUNCTION rt_task_child();

CREATE TRIGGER rt_spaces AFTER INSERT OR UPDATE OR DELETE ON spaces FOR EACH ROW EXECUTE FUNCTION rt_space('id');
CREATE TRIGGER rt_lists AFTER INSERT OR UPDATE OR DELETE ON lists FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_statuses AFTER INSERT OR UPDATE OR DELETE ON statuses FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_folders AFTER INSERT OR UPDATE OR DELETE ON folders FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_tags AFTER INSERT OR UPDATE OR DELETE ON tags FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_grants AFTER INSERT OR UPDATE OR DELETE ON access_grants FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');
CREATE TRIGGER rt_automations AFTER INSERT OR UPDATE OR DELETE ON automations FOR EACH ROW EXECUTE FUNCTION rt_space('space_id');

CREATE TRIGGER rt_notifications AFTER INSERT OR UPDATE OR DELETE ON notifications FOR EACH ROW EXECUTE FUNCTION rt_inbox();

-- +goose Down
DROP TRIGGER IF EXISTS rt_notifications ON notifications;
DROP TRIGGER IF EXISTS rt_automations ON automations;
DROP TRIGGER IF EXISTS rt_grants ON access_grants;
DROP TRIGGER IF EXISTS rt_tags ON tags;
DROP TRIGGER IF EXISTS rt_folders ON folders;
DROP TRIGGER IF EXISTS rt_statuses ON statuses;
DROP TRIGGER IF EXISTS rt_lists ON lists;
DROP TRIGGER IF EXISTS rt_spaces ON spaces;
DROP TRIGGER IF EXISTS rt_attachments ON attachments;
DROP TRIGGER IF EXISTS rt_watchers ON task_watchers;
DROP TRIGGER IF EXISTS rt_task_tags ON task_tags;
DROP TRIGGER IF EXISTS rt_assignees ON task_assignees;
DROP TRIGGER IF EXISTS rt_checklist ON checklist_items;
DROP TRIGGER IF EXISTS rt_comments ON comments;
DROP TRIGGER IF EXISTS rt_tasks ON tasks;
DROP FUNCTION IF EXISTS rt_inbox();
DROP FUNCTION IF EXISTS rt_space();
DROP FUNCTION IF EXISTS rt_task_child();
DROP FUNCTION IF EXISTS rt_tasks();
DROP FUNCTION IF EXISTS rt_notify(jsonb);
