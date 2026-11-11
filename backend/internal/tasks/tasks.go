package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// TaskStatus is the status embedded in a task row.
type TaskStatus struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Color    string    `json:"color"`
	Category string    `json:"category"`
}

// Task is a task (or subtask) row as the UI shows it.
type Task struct {
	ID           uuid.UUID  `json:"id"`
	Key          string     `json:"key"`
	Number       int        `json:"number"`
	SpaceID      uuid.UUID  `json:"space_id"`
	ListID       uuid.UUID  `json:"list_id"`
	ListName     string     `json:"list_name"`
	ParentID     *uuid.UUID `json:"parent_id"`
	ParentKey    *string    `json:"parent_key"`
	ParentTitle  *string    `json:"parent_title"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Status       TaskStatus `json:"status"`
	Priority     *string    `json:"priority"`
	StartAt      *string    `json:"start_at"`
	DueAt        *string    `json:"due_at"`
	Position     float64    `json:"position"`
	CompletedAt  *time.Time `json:"completed_at"`
	Version      int        `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	Assignees    []Person   `json:"assignees"`
	Tags         []Tag      `json:"tags"`
	SubtaskCount int        `json:"subtask_count"`
	SubtaskDone  int        `json:"subtask_done"`
	CommentCount int        `json:"comment_count"`
	CheckTotal   int        `json:"checklist_total"`
	CheckDone    int        `json:"checklist_done"`
	// Fields maps custom field id → value (see fields.go).
	Fields map[string]json.RawMessage `json:"fields"`
}

// The per-task aggregates come from lateral joins instead of four
// correlated scalar subqueries in the select list (TASK-38): one pass per
// task over assignees, one over subtasks (both counts), one over comments.
const taskSelect = `
SELECT t.id, sp.key || '-' || t.number, t.number, t.space_id, t.list_id, l.name,
       t.parent_id, CASE WHEN pt.id IS NULL THEN NULL ELSE sp.key || '-' || pt.number END, pt.title,
       t.title, t.description, st.id, st.name, st.color, st.category, t.priority,
       to_char(t.start_at, 'YYYY-MM-DD'), to_char(t.due_at, 'YYYY-MM-DD'),
       t.position, t.completed_at, t.version, t.created_at, t.updated_at, t.created_by,
       COALESCE(asg.people, '[]'::json), COALESCE(tgs.tags, '[]'::json),
       COALESCE(sub.total, 0), COALESCE(sub.done, 0), COALESCE(cmt.n, 0),
       COALESCE(chk.total, 0), COALESCE(chk.done, 0), COALESCE(fvs.m, '{}'::json)
  FROM tasks t
  JOIN spaces sp ON sp.id = t.space_id
  JOIN lists l ON l.id = t.list_id
  JOIN statuses st ON st.id = t.status_id
  LEFT JOIN tasks pt ON pt.id = t.parent_id
  LEFT JOIN LATERAL (
       SELECT json_agg(json_build_object('id', u.id, 'display_name', u.display_name) ORDER BY u.display_name) AS people
         FROM task_assignees a JOIN users u ON u.id = a.user_id WHERE a.task_id = t.id) asg ON true
  LEFT JOIN LATERAL (
       SELECT json_agg(json_build_object('id', tg.id, 'name', tg.name, 'color', tg.color) ORDER BY tg.name) AS tags
         FROM task_tags tt JOIN tags tg ON tg.id = tt.tag_id WHERE tt.task_id = t.id) tgs ON true
  LEFT JOIN LATERAL (
       SELECT count(*) AS total, count(*) FILTER (WHERE cs.category = 'done') AS done
         FROM tasks c JOIN statuses cs ON cs.id = c.status_id WHERE c.parent_id = t.id AND c.deleted_at IS NULL) sub ON true
  LEFT JOIN LATERAL (
       SELECT count(*) AS n FROM comments cm WHERE cm.task_id = t.id) cmt ON true
  LEFT JOIN LATERAL (
       SELECT count(*) AS total, count(*) FILTER (WHERE ci.done) AS done
         FROM checklist_items ci WHERE ci.task_id = t.id) chk ON true
  LEFT JOIN LATERAL (
       SELECT json_object_agg(fv.field_id, fv.value) AS m
         FROM task_field_values fv WHERE fv.task_id = t.id) fvs ON true`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	var assignees, tags, fields []byte
	err := row.Scan(&t.ID, &t.Key, &t.Number, &t.SpaceID, &t.ListID, &t.ListName,
		&t.ParentID, &t.ParentKey, &t.ParentTitle,
		&t.Title, &t.Description, &t.Status.ID, &t.Status.Name, &t.Status.Color, &t.Status.Category, &t.Priority,
		&t.StartAt, &t.DueAt, &t.Position, &t.CompletedAt, &t.Version, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy,
		&assignees, &tags, &t.SubtaskCount, &t.SubtaskDone, &t.CommentCount, &t.CheckTotal, &t.CheckDone, &fields)
	if err != nil {
		return Task{}, err
	}
	t.Assignees = []Person{}
	if err := json.Unmarshal(assignees, &t.Assignees); err != nil {
		return Task{}, err
	}
	t.Tags = []Tag{}
	if err := json.Unmarshal(tags, &t.Tags); err != nil {
		return Task{}, err
	}
	t.Fields = map[string]json.RawMessage{}
	if err := json.Unmarshal(fields, &t.Fields); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *Service) queryTasks(ctx context.Context, where string, args ...any) ([]Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Pagination (TASK-38). Responses keep their array shape; when a page is
// full the service returns an opaque cursor the handler exposes as the
// X-Next-Cursor header. Clients that never send a cursor still get the
// first `limit` rows, so old callers keep working.

// DefaultPageLimit is used when the caller does not pick a page size.
const DefaultPageLimit = 200

// MaxPageLimit caps the page size a caller can request.
const MaxPageLimit = 500

// ClampLimit normalizes a requested page size.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// listCursor is the keyset position of the last row of a page in board
// order (position, number). Number is unique per space, so the pair is a
// total order even when positions collide.
type listCursor struct {
	Position float64 `json:"p"`
	Number   int     `json:"n"`
}

// offsetCursor pages queries whose sort order is too mixed for a keyset
// (MyWork sorts on done-ness, due date and recency).
type offsetCursor struct {
	Offset int `json:"o"`
}

func encodeCursor(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string, into any) error {
	if s == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(raw, into)
	}
	if err != nil {
		return httperror.BadRequest("invalid cursor")
	}
	return nil
}

// ListTasks returns one page of a list's tasks and subtasks in board order,
// plus the cursor for the next page ("" on the last page).
func (s *Service) ListTasks(ctx context.Context, listID uuid.UUID, limit int, cursor string) ([]Task, string, error) {
	limit = ClampLimit(limit)
	var c listCursor
	if err := decodeCursor(cursor, &c); err != nil {
		return nil, "", err
	}
	out, err := s.queryTasks(ctx, `
		WHERE t.list_id = $1 AND t.deleted_at IS NULL
		  AND ($2::bool OR (t.position, t.number) > ($3::float8, $4::int))
		ORDER BY t.position, t.number LIMIT $5`,
		listID, cursor == "", c.Position, c.Number, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(listCursor{Position: last.Position, Number: last.Number})
	}
	return out, next, nil
}

// MyWork returns one page of open tasks assigned to the user plus ones they
// finished in the last week, soonest due first, and the next-page cursor.
func (s *Service) MyWork(ctx context.Context, userID uuid.UUID, limit int, cursor string) ([]Task, string, error) {
	limit = ClampLimit(limit)
	var c offsetCursor
	if err := decodeCursor(cursor, &c); err != nil {
		return nil, "", err
	}
	out, err := s.queryTasks(ctx, `
		WHERE EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id = t.id AND a.user_id = $1)
		  AND t.deleted_at IS NULL
		  AND (st.category <> 'done' OR t.completed_at > now() - interval '7 days')
		ORDER BY st.category = 'done', t.due_at NULLS LAST, t.updated_at DESC, t.id
		OFFSET $2 LIMIT $3`, userID, c.Offset, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = encodeCursor(offsetCursor{Offset: c.Offset + limit})
	}
	return out, next, nil
}

var refPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,5})-(\d+)$`)

// ResolveTask accepts a uuid or a KEY-N reference and returns the task id.
// References from before a cross-space move keep resolving through the
// task_ids alias table (TASK-45).
func (s *Service) ResolveTask(ctx context.Context, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	m := refPattern.FindStringSubmatch(ref)
	if m == nil {
		return uuid.Nil, httperror.NotFound("task")
	}
	n, _ := strconv.Atoi(m[2])
	key := strings.ToUpper(m[1])
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT t.id FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE sp.key = $1 AND t.number = $2`,
		key, n).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT task_id FROM task_ids WHERE key = $1`, key+"-"+m[2]).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.NotFound("task")
	}
	return id, err
}

// GetTask loads one task or a typed 404. Trashed tasks are 404 here; the
// trash view uses TrashedTasks and restore goes through RestoreTask.
func (s *Service) GetTask(ctx context.Context, id uuid.UUID) (Task, error) {
	t, err := scanTask(s.pool.QueryRow(ctx, taskSelect+` WHERE t.id = $1 AND t.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, httperror.NotFound("task")
	}
	return t, err
}

// Comment is a message on a task.
type Comment struct {
	ID         uuid.UUID  `json:"id"`
	AuthorID   *uuid.UUID `json:"author_id"`
	AuthorName string     `json:"author_name"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"created_at"`
	EditedAt   *time.Time `json:"edited_at"`
}

// Event is one activity-log entry.
type Event struct {
	ID        uuid.UUID       `json:"id"`
	ActorName string          `json:"actor_name"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// TaskDetail is a task with its subtasks, comments and activity.
type TaskDetail struct {
	Task
	Subtasks    []Task          `json:"subtasks"`
	Comments    []Comment       `json:"comments"`
	Events      []Event         `json:"events"`
	Statuses    []Status        `json:"statuses"`
	Checklist   []ChecklistItem `json:"checklist"`
	Watchers    []Person        `json:"watchers"`
	Attachments []Attachment    `json:"attachments"`
	// CommentsTotal and EventsTotal count everything; Comments and Events
	// hold only the newest DetailComments / DetailEvents.
	CommentsTotal int `json:"comments_total"`
	EventsTotal   int `json:"events_total"`
}

// Detail loads the full task page.
func (s *Service) Detail(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	d := TaskDetail{Task: t}
	if d.Subtasks, err = s.queryTasks(ctx, `WHERE t.parent_id = $1 AND t.deleted_at IS NULL ORDER BY t.position, t.number`, id); err != nil {
		return d, err
	}
	if d.Checklist, err = s.Checklist(ctx, id); err != nil {
		return d, err
	}
	if d.Watchers, err = s.Watchers(ctx, id); err != nil {
		return d, err
	}
	if d.Attachments, err = s.Attachments(ctx, id); err != nil {
		return d, err
	}
	if d.Statuses, err = s.statuses(ctx, s.pool, t.SpaceID); err != nil {
		return d, err
	}
	// The task page shows the newest comments and events; older ones are
	// paged through Comments and Events.
	if d.Comments, err = s.scanComments(ctx, `
		SELECT * FROM (`+commentSelect+` WHERE c.task_id = $1 ORDER BY c.created_at DESC, c.id DESC LIMIT $2) x
		 ORDER BY 5, 1`, id, DetailComments); err != nil {
		return d, err
	}
	if d.Events, err = s.scanEvents(ctx, `
		SELECT * FROM (`+eventSelect+` WHERE e.task_id = $1 ORDER BY e.created_at DESC, e.id DESC LIMIT $2) x
		 ORDER BY 5, 1`, id, DetailEvents); err != nil {
		return d, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM comments WHERE task_id = $1), (SELECT count(*) FROM task_events WHERE task_id = $1)`, id,
	).Scan(&d.CommentsTotal, &d.EventsTotal)
	return d, err
}

// How many comments and events the task detail embeds (newest first).
const (
	DetailComments = 100
	DetailEvents   = 200
)

const commentSelect = `
	SELECT c.id, c.author_id,
	       -- NULL author: an automation (named) or a deleted user.
	       COALESCE(u.display_name, 'Automation “' || NULLIF(c.via_automation, '') || '”', 'Former user'),
	       c.body, c.created_at, c.edited_at
	  FROM comments c LEFT JOIN users u ON u.id = c.author_id`

const eventSelect = `
	SELECT e.id, COALESCE(u.display_name, 'Someone'), e.kind, e.data, e.created_at
	  FROM task_events e LEFT JOIN users u ON u.id = e.actor_id`

func (s *Service) scanComments(ctx context.Context, sql string, args ...any) ([]Comment, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.EditedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) scanEvents(ctx context.Context, sql string, args ...any) ([]Event, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var data []byte
		if err := rows.Scan(&e.ID, &e.ActorName, &e.Kind, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = data
		out = append(out, e)
	}
	return out, rows.Err()
}

// timeCursor is the keyset position (created_at, id) of the last row of a
// page in oldest-first order.
type timeCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

// Comments returns one page of a task's comments, oldest first.
func (s *Service) Comments(ctx context.Context, taskID uuid.UUID, limit int, cursor string) ([]Comment, string, error) {
	limit = ClampLimit(limit)
	var c timeCursor
	if err := decodeCursor(cursor, &c); err != nil {
		return nil, "", err
	}
	out, err := s.scanComments(ctx, commentSelect+`
		WHERE c.task_id = $1 AND ($2::bool OR (c.created_at, c.id) > ($3, $4))
		ORDER BY c.created_at, c.id LIMIT $5`, taskID, cursor == "", c.At, c.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(timeCursor{At: last.CreatedAt, ID: last.ID})
	}
	return out, next, nil
}

// Events returns one page of a task's activity, oldest first.
func (s *Service) Events(ctx context.Context, taskID uuid.UUID, limit int, cursor string) ([]Event, string, error) {
	limit = ClampLimit(limit)
	var c timeCursor
	if err := decodeCursor(cursor, &c); err != nil {
		return nil, "", err
	}
	out, err := s.scanEvents(ctx, eventSelect+`
		WHERE e.task_id = $1 AND ($2::bool OR (e.created_at, e.id) > ($3, $4))
		ORDER BY e.created_at, e.id LIMIT $5`, taskID, cursor == "", c.At, c.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(timeCursor{At: last.CreatedAt, ID: last.ID})
	}
	return out, next, nil
}

// Nullable distinguishes "absent" from "explicitly null" in a PATCH body.
type Nullable struct {
	Set   bool
	Value *string
}

// UnmarshalJSON records that the field was present.
func (n *Nullable) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

// NullableUUID is Nullable for uuid fields (e.g. a list's folder: absent =
// keep, null = move to the space root).
type NullableUUID struct {
	Set   bool
	Value *uuid.UUID
}

// UnmarshalJSON records that the field was present.
func (n *NullableUUID) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	var v uuid.UUID
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

// TaskInput creates a task.
type TaskInput struct {
	ListID      *uuid.UUID  `json:"list_id"`
	ParentID    *uuid.UUID  `json:"parent_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	StatusID    *uuid.UUID  `json:"status_id"`
	Priority    *string     `json:"priority"`
	StartAt     *string     `json:"start_at"`
	DueAt       *string     `json:"due_at"`
	AssigneeIDs []uuid.UUID `json:"assignee_ids"`
}

func validPriority(p *string) bool {
	if p == nil {
		return true
	}
	for _, v := range domain.Priorities {
		if v == *p {
			return true
		}
	}
	return false
}

func validDate(d *string) bool {
	if d == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", *d)
	return err == nil
}

// Field length caps (TASK-39). Generous for real use, tight enough that a
// single request cannot bloat a row unboundedly.
const (
	maxTitleLen       = 500
	maxDescriptionLen = 100_000
	maxCommentLen     = 20_000
)

// CreateTask adds a task at the bottom of its list. A subtask inherits its
// parent's list.
func (s *Service) CreateTask(ctx context.Context, actor uuid.UUID, in TaskInput) (Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	issues := map[string]string{}
	if in.Title == "" {
		issues["title"] = "required"
	}
	if len(in.Title) > maxTitleLen {
		issues["title"] = "too long"
	}
	if len(in.Description) > maxDescriptionLen {
		issues["description"] = "too long"
	}
	if in.ListID == nil && in.ParentID == nil {
		issues["list_id"] = "required"
	}
	if !validPriority(in.Priority) {
		issues["priority"] = "must be urgent, high, normal or low"
	}
	if !validDate(in.StartAt) {
		issues["start_at"] = "must be YYYY-MM-DD"
	}
	if !validDate(in.DueAt) {
		issues["due_at"] = "must be YYYY-MM-DD"
	}
	if len(issues) > 0 {
		return Task{}, httperror.Validation("invalid task", issues)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var listID, spaceID uuid.UUID
	if in.ParentID != nil {
		var parentParent *uuid.UUID
		err = tx.QueryRow(ctx, `SELECT list_id, space_id, parent_id FROM tasks WHERE id = $1`, *in.ParentID).Scan(&listID, &spaceID, &parentParent)
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, httperror.NotFound("parent task")
		}
		if err == nil && parentParent != nil {
			return Task{}, httperror.Validation("subtasks cannot have their own subtasks", nil)
		}
	} else {
		listID = *in.ListID
		err = tx.QueryRow(ctx, `SELECT space_id FROM lists WHERE id = $1 AND archived_at IS NULL`, listID).Scan(&spaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, httperror.NotFound("list")
		}
	}
	if err != nil {
		return Task{}, err
	}

	var statusID uuid.UUID
	var category string
	if in.StatusID != nil {
		err = tx.QueryRow(ctx, `SELECT id, category FROM statuses WHERE id = $1 AND space_id = $2`, *in.StatusID, spaceID).Scan(&statusID, &category)
	} else {
		err = tx.QueryRow(ctx, `SELECT id, category FROM statuses WHERE space_id = $1 ORDER BY category <> 'not_started', position LIMIT 1`, spaceID).Scan(&statusID, &category)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, httperror.Validation("status does not belong to this space", nil)
	}
	if err != nil {
		return Task{}, err
	}

	// Validate assignees up front (TASK-35): an unknown id used to surface
	// as a foreign-key violation and a 500.
	if len(in.AssigneeIDs) > 0 {
		var known int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY($1)`, in.AssigneeIDs).Scan(&known); err != nil {
			return Task{}, err
		}
		distinct := map[uuid.UUID]bool{}
		for _, uid := range in.AssigneeIDs {
			distinct[uid] = true
		}
		if known != len(distinct) {
			return Task{}, httperror.Validation("unknown assignee", map[string]string{"assignee_ids": "contains a user that does not exist"})
		}
	}

	var number int
	if err := tx.QueryRow(ctx, `UPDATE spaces SET next_number = next_number + 1 WHERE id = $1 RETURNING next_number - 1`, spaceID).Scan(&number); err != nil {
		return Task{}, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks (space_id, list_id, parent_id, number, title, description, status_id, priority,
		                   start_at, due_at, position, created_by, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, $10::date,
		        (SELECT COALESCE(max(position), 0) + 1024 FROM tasks WHERE list_id = $2), $11,
		        CASE WHEN $12 = 'done' THEN now() END)
		RETURNING id`,
		spaceID, listID, in.ParentID, number, in.Title, in.Description, statusID, in.Priority,
		in.StartAt, in.DueAt, actor, category).Scan(&id)
	if err != nil {
		return Task{}, err
	}
	for _, uid := range in.AssigneeIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO task_assignees (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
			return Task{}, err
		}
	}
	if err := addEvent(ctx, tx, id, actor, "created", nil); err != nil {
		return Task{}, err
	}
	// Automations: "when a task is created…" (TASK-22).
	if err := s.fireAutomations(ctx, tx, id, &actor, autoTrigger{kind: "task_created"}, map[uuid.UUID]bool{}, 0); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, id)
}

// TaskPatch updates a task. Absent fields are left alone; Nullable fields
// sent as null are cleared.
type TaskPatch struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	StatusID    *uuid.UUID   `json:"status_id"`
	ListID      *uuid.UUID   `json:"list_id"`
	Position    *float64     `json:"position"`
	Priority    Nullable     `json:"priority"`
	StartAt     Nullable     `json:"start_at"`
	DueAt       Nullable     `json:"due_at"`
	AssigneeIDs *[]uuid.UUID `json:"assignee_ids"`
	TagIDs      *[]uuid.UUID `json:"tag_ids"`
	// Fields sets custom field values by field id; null clears one.
	Fields map[uuid.UUID]json.RawMessage `json:"fields"`
	Version     *int         `json:"version"`
}

func addEvent(ctx context.Context, tx pgx.Tx, taskID, actor uuid.UUID, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// The key/title snapshot keeps the event meaningful after the task is
	// deleted (the FK sets task_id to NULL instead of cascading, TASK-29).
	_, err = tx.Exec(ctx, `
		INSERT INTO task_events (task_id, actor_id, kind, data, task_key, task_title)
		SELECT t.id, $2, $3, $4, sp.key || '-' || t.number, t.title
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $1`, taskID, actor, kind, raw)
	return err
}

// writeAudit records a destructive or access-changing action in the
// audit_log (TASK-29). actor is nil for the system (retention sweep, HR's
// department hook).
func writeAudit(ctx context.Context, tx pgx.Tx, actor *uuid.UUID, entity string, entityID uuid.UUID, action string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log (actor_id, entity, entity_id, action, data) VALUES ($1, $2, $3, $4, $5)`,
		actor, entity, entityID, action, raw)
	return err
}

// UpdateTask applies a patch and logs each change to the activity log.
// The caller must send the version it last saw; the bump at the end is
// conditional on that version (TASK-36), so two concurrent PATCHes cannot
// silently overwrite each other — the loser gets a 409.
func (s *Service) UpdateTask(ctx context.Context, actor, id uuid.UUID, p TaskPatch) (Task, error) {
	cur, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if p.Version == nil {
		return Task{}, httperror.Validation("invalid task", map[string]string{"version": "required"})
	}
	if *p.Version != cur.Version {
		return Task{}, httperror.Conflict("someone else changed this task — reload and try again")
	}
	issues := map[string]string{}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		p.Title = &t
		if t == "" {
			issues["title"] = "required"
		}
		if len(t) > maxTitleLen {
			issues["title"] = "too long"
		}
	}
	if p.Description != nil && len(*p.Description) > maxDescriptionLen {
		issues["description"] = "too long"
	}
	if p.Priority.Set && !validPriority(p.Priority.Value) {
		issues["priority"] = "must be urgent, high, normal or low"
	}
	if p.StartAt.Set && !validDate(p.StartAt.Value) {
		issues["start_at"] = "must be YYYY-MM-DD"
	}
	if p.DueAt.Set && !validDate(p.DueAt.Value) {
		issues["due_at"] = "must be YYYY-MM-DD"
	}
	if len(issues) > 0 {
		return Task{}, httperror.Validation("invalid task", issues)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Automation triggers collected along the way, fired before commit.
	var statusTrig *autoTrigger
	assigneeAdded := false

	if p.Title != nil && *p.Title != cur.Title {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET title = $2 WHERE id = $1`, id, *p.Title); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "title", map[string]any{"from": cur.Title, "to": *p.Title}); err != nil {
			return Task{}, err
		}
	}
	if p.Description != nil && *p.Description != cur.Description {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET description = $2 WHERE id = $1`, id, *p.Description); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "description", nil); err != nil {
			return Task{}, err
		}
	}
	if p.StatusID != nil && *p.StatusID != cur.Status.ID {
		var name, category string
		err := tx.QueryRow(ctx, `SELECT name, category FROM statuses WHERE id = $1 AND space_id = $2`, *p.StatusID, cur.SpaceID).Scan(&name, &category)
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, httperror.Validation("status does not belong to this space", nil)
		}
		if err != nil {
			return Task{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE tasks SET status_id = $2,
			       completed_at = CASE WHEN $3 = 'done' THEN COALESCE(completed_at, now()) ELSE NULL END
			 WHERE id = $1`, id, *p.StatusID, category); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "status", map[string]any{"from": cur.Status.Name, "to": name}); err != nil {
			return Task{}, err
		}
		if err := s.notifyFollowers(ctx, tx, id, actor, "status", "moved to "+name); err != nil {
			return Task{}, err
		}
		statusTrig = &autoTrigger{kind: "status_changed", statusID: *p.StatusID, category: category}
	}
	if p.ListID != nil && *p.ListID != cur.ListID {
		if cur.ParentID != nil {
			return Task{}, httperror.Validation("subtasks stay in their parent's list", nil)
		}
		var name string
		var targetSpace uuid.UUID
		err := tx.QueryRow(ctx, `SELECT name, space_id FROM lists WHERE id = $1 AND archived_at IS NULL`, *p.ListID).Scan(&name, &targetSpace)
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, httperror.Validation("unknown list", nil)
		}
		if err != nil {
			return Task{}, err
		}
		if targetSpace == cur.SpaceID {
			if _, err := tx.Exec(ctx, `UPDATE tasks SET list_id = $2 WHERE id = $1 OR parent_id = $1`, id, *p.ListID); err != nil {
				return Task{}, err
			}
		} else if err := moveAcrossSpaces(ctx, tx, id, *p.ListID, targetSpace); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "list", map[string]any{"from": cur.ListName, "to": name}); err != nil {
			return Task{}, err
		}
	}
	if p.Position != nil {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET position = $2 WHERE id = $1`, id, *p.Position); err != nil {
			return Task{}, err
		}
		// Drag-and-drop halves the gap between neighbours every drop; once
		// a gap shrinks below float precision the whole list is renumbered
		// back to 1024-spaced positions (TASK-37). Versions are not bumped:
		// the relative order is unchanged, so concurrent edits stay valid.
		listID := cur.ListID
		if p.ListID != nil {
			listID = *p.ListID
		}
		var cramped bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM (
					SELECT position - lag(position) OVER (ORDER BY position, number) AS gap
					  FROM tasks WHERE list_id = $1 AND parent_id IS NULL) g
				 WHERE g.gap < 1e-9)`, listID).Scan(&cramped); err != nil {
			return Task{}, err
		}
		if cramped {
			if _, err := tx.Exec(ctx, `
				UPDATE tasks t SET position = s.rn * 1024
				  FROM (SELECT id, row_number() OVER (ORDER BY position, number) AS rn
				          FROM tasks WHERE list_id = $1 AND parent_id IS NULL) s
				 WHERE t.id = s.id`, listID); err != nil {
				return Task{}, err
			}
		}
	}
	if p.Priority.Set && !sameStr(p.Priority.Value, cur.Priority) {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET priority = $2 WHERE id = $1`, id, p.Priority.Value); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "priority", map[string]any{"from": cur.Priority, "to": p.Priority.Value}); err != nil {
			return Task{}, err
		}
	}
	if p.StartAt.Set && !sameStr(p.StartAt.Value, cur.StartAt) {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET start_at = $2::date WHERE id = $1`, id, p.StartAt.Value); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "start_at", map[string]any{"from": cur.StartAt, "to": p.StartAt.Value}); err != nil {
			return Task{}, err
		}
	}
	if p.DueAt.Set && !sameStr(p.DueAt.Value, cur.DueAt) {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET due_at = $2::date WHERE id = $1`, id, p.DueAt.Value); err != nil {
			return Task{}, err
		}
		if err := addEvent(ctx, tx, id, actor, "due_at", map[string]any{"from": cur.DueAt, "to": p.DueAt.Value}); err != nil {
			return Task{}, err
		}
	}
	if p.AssigneeIDs != nil {
		before := map[uuid.UUID]string{}
		for _, a := range cur.Assignees {
			before[a.ID] = a.DisplayName
		}
		after := map[uuid.UUID]bool{}
		for _, uid := range *p.AssigneeIDs {
			after[uid] = true
			if _, ok := before[uid]; ok {
				continue
			}
			var name string
			err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, uid).Scan(&name)
			if errors.Is(err, pgx.ErrNoRows) {
				return Task{}, httperror.Validation("unknown assignee", nil)
			}
			if err != nil {
				return Task{}, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO task_assignees (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
				return Task{}, err
			}
			if err := addEvent(ctx, tx, id, actor, "assigned", map[string]any{"user": name}); err != nil {
				return Task{}, err
			}
			if err := s.notify(ctx, tx, id, actor, "assigned", "assigned this task to you", []uuid.UUID{uid}); err != nil {
				return Task{}, err
			}
			assigneeAdded = true
		}
		for uid, name := range before {
			if after[uid] {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM task_assignees WHERE task_id = $1 AND user_id = $2`, id, uid); err != nil {
				return Task{}, err
			}
			if err := addEvent(ctx, tx, id, actor, "unassigned", map[string]any{"user": name}); err != nil {
				return Task{}, err
			}
		}
	}
	if p.TagIDs != nil {
		// Tags belong to the task's space (the target space if this patch
		// also moved it).
		var spaceID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT space_id FROM tasks WHERE id = $1`, id).Scan(&spaceID); err != nil {
			return Task{}, err
		}
		if len(*p.TagIDs) > 0 {
			var known int
			if err := tx.QueryRow(ctx, `SELECT count(DISTINCT id) FROM tags WHERE id = ANY($1) AND space_id = $2`,
				*p.TagIDs, spaceID).Scan(&known); err != nil {
				return Task{}, err
			}
			distinct := map[uuid.UUID]bool{}
			for _, tid := range *p.TagIDs {
				distinct[tid] = true
			}
			if known != len(distinct) {
				return Task{}, httperror.Validation("unknown tag", map[string]string{"tag_ids": "contains a tag that is not in this space"})
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_tags WHERE task_id = $1`, id); err != nil {
			return Task{}, err
		}
		for _, tid := range *p.TagIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, tid); err != nil {
				return Task{}, err
			}
		}
		if err := addEvent(ctx, tx, id, actor, "tags", nil); err != nil {
			return Task{}, err
		}
	}
	if len(p.Fields) > 0 {
		// Fields belong to the task's space (the target space if this
		// patch also moved it).
		var spaceID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT space_id FROM tasks WHERE id = $1`, id).Scan(&spaceID); err != nil {
			return Task{}, err
		}
		if err := s.setFieldValues(ctx, tx, actor, id, spaceID, p.Fields); err != nil {
			return Task{}, err
		}
	}
	// Atomic optimistic lock: bump only if nobody committed in between the
	// GetTask above and here. Zero rows means a concurrent writer won; the
	// transaction rolls back and the caller gets a conflict.
	tag, err := tx.Exec(ctx, `UPDATE tasks SET updated_at = now(), version = version + 1 WHERE id = $1 AND version = $2`, id, *p.Version)
	if err != nil {
		return Task{}, err
	}
	if tag.RowsAffected() == 0 {
		return Task{}, httperror.Conflict("someone else changed this task — reload and try again")
	}
	// Automations fire inside this transaction, after the patch itself, so
	// rules see the task as the user left it (TASK-22).
	visited := map[uuid.UUID]bool{}
	if statusTrig != nil {
		if err := s.fireAutomations(ctx, tx, id, &actor, *statusTrig, visited, 0); err != nil {
			return Task{}, err
		}
	}
	if assigneeAdded {
		if err := s.fireAutomations(ctx, tx, id, &actor, autoTrigger{kind: "assignee_added"}, visited, 0); err != nil {
			return Task{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, id)
}

// moveAcrossSpaces moves a task and its subtasks to a list in another space
// (TASK-10). Each moved task gets a fresh number in the target space; the
// old KEY-N goes into task_ids so stale references keep resolving
// (TASK-45). Statuses remap by category; tags stay behind, because tags
// belong to a space.
func moveAcrossSpaces(ctx context.Context, tx pgx.Tx, id, targetList, targetSpace uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT t.id, sp.key || '-' || t.number, st.category
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id JOIN statuses st ON st.id = t.status_id
		 WHERE t.id = $1 OR t.parent_id = $1`, id)
	if err != nil {
		return err
	}
	type moved struct {
		id       uuid.UUID
		oldKey   string
		category string
	}
	var all []moved
	for rows.Next() {
		var m moved
		if err := rows.Scan(&m.id, &m.oldKey, &m.category); err != nil {
			rows.Close()
			return err
		}
		all = append(all, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// One status per category in the target space, picked once.
	statusFor := map[string]uuid.UUID{}
	var firstStatus uuid.UUID
	srows, err := tx.Query(ctx, `SELECT id, category FROM statuses WHERE space_id = $1 ORDER BY position`, targetSpace)
	if err != nil {
		return err
	}
	for srows.Next() {
		var sid uuid.UUID
		var cat string
		if err := srows.Scan(&sid, &cat); err != nil {
			srows.Close()
			return err
		}
		if firstStatus == uuid.Nil {
			firstStatus = sid
		}
		if _, ok := statusFor[cat]; !ok {
			statusFor[cat] = sid
		}
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return err
	}

	var base int
	if err := tx.QueryRow(ctx, `UPDATE spaces SET next_number = next_number + $2 WHERE id = $1 RETURNING next_number - $2`,
		targetSpace, len(all)).Scan(&base); err != nil {
		return err
	}
	for i, m := range all {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_ids (key, task_id) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET task_id = EXCLUDED.task_id`, m.oldKey, m.id); err != nil {
			return err
		}
		status, ok := statusFor[m.category]
		if !ok {
			status = firstStatus
		}
		if _, err := tx.Exec(ctx, `
			UPDATE tasks SET space_id = $2, list_id = $3, number = $4, status_id = $5,
			       position = (SELECT COALESCE(max(position), 0) + 1024 FROM tasks WHERE list_id = $3)
			 WHERE id = $1`, m.id, targetSpace, targetList, base+i, status); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_tags WHERE task_id = $1`, m.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_field_values WHERE task_id = $1`, m.id); err != nil {
			return err
		}
	}
	return nil
}

func sameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// DeleteTask moves a task and its subtasks to the trash (TASK-48): they
// disappear from every view but can be restored with RestoreTask. An
// audit_log row and a "deleted" activity event are written immediately, so
// the trail does not depend on an eventual purge.
func (s *Service) DeleteTask(ctx context.Context, actor, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var key, title string
	var subtasks int
	err = tx.QueryRow(ctx, `
		SELECT sp.key || '-' || t.number, t.title,
		       (SELECT count(*) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at IS NULL)
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $1 AND t.deleted_at IS NULL`, id).Scan(&key, &title, &subtasks)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("task")
	}
	if err != nil {
		return err
	}
	if err := addEvent(ctx, tx, id, actor, "deleted", nil); err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, &actor, "task", id, "deleted", map[string]any{"key": key, "title": title, "subtasks": subtasks}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tasks SET deleted_at = now(), deleted_by = $2
		 WHERE (id = $1 OR parent_id = $1) AND deleted_at IS NULL`, id, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TrashedTask is a trash-view row.
type TrashedTask struct {
	ID        uuid.UUID  `json:"id"`
	Key       string     `json:"key"`
	Title     string     `json:"title"`
	ListName  string     `json:"list_name"`
	DeletedAt time.Time  `json:"deleted_at"`
	DeletedBy *uuid.UUID `json:"deleted_by"`
}

// TrashedTasks lists a space's trashed top-level tasks, newest first.
func (s *Service) TrashedTasks(ctx context.Context, spaceID uuid.UUID) ([]TrashedTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, sp.key || '-' || t.number, t.title, l.name, t.deleted_at, t.deleted_by
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id JOIN lists l ON l.id = t.list_id
		 WHERE t.space_id = $1 AND t.deleted_at IS NOT NULL AND t.parent_id IS NULL
		 ORDER BY t.deleted_at DESC LIMIT 200`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashedTask{}
	for rows.Next() {
		var t TrashedTask
		if err := rows.Scan(&t.ID, &t.Key, &t.Title, &t.ListName, &t.DeletedAt, &t.DeletedBy); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TaskSpace returns the space of a task, trashed or not (for trash-route
// access checks).
func (s *Service) TaskSpace(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var spaceID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT space_id FROM tasks WHERE id = $1`, id).Scan(&spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.NotFound("task")
	}
	return spaceID, err
}

// RestoreTask brings a trashed task (and the subtasks trashed with it)
// back. If its list was deleted in the meantime the restore fails.
func (s *Service) RestoreTask(ctx context.Context, actor, id uuid.UUID) (Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var listGone bool
	err = tx.QueryRow(ctx, `
		SELECT l.archived_at IS NOT NULL FROM tasks t JOIN lists l ON l.id = t.list_id
		 WHERE t.id = $1 AND t.deleted_at IS NOT NULL`, id).Scan(&listGone)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, httperror.NotFound("task")
	}
	if err != nil {
		return Task{}, err
	}
	if listGone {
		return Task{}, httperror.Conflict("the task's list is archived — restore the list first")
	}
	if _, err := tx.Exec(ctx, `UPDATE tasks SET deleted_at = NULL, deleted_by = NULL WHERE id = $1 OR parent_id = $1`, id); err != nil {
		return Task{}, err
	}
	if err := addEvent(ctx, tx, id, actor, "restored", nil); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, id)
}

// PurgeTask permanently deletes a trashed task (manage only; the UI calls
// this from the trash view). The audit row from the original delete stays.
// It returns the S3 keys of the task's attachments for the caller to
// remove.
func (s *Service) PurgeTask(ctx context.Context, actor, id uuid.UUID) ([]string, error) {
	return s.purge(ctx, &actor, id, nil)
}

// AddComment posts a comment on a task. Mentioned users get a "mention"
// Inbox notification; watchers and assignees get a "comment" one (TASK-5).
func (s *Service) AddComment(ctx context.Context, actor, taskID uuid.UUID, body string, mentions []uuid.UUID) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, httperror.Validation("comment is empty", nil)
	}
	if len(body) > maxCommentLen {
		return Comment{}, httperror.Validation("comment is too long", nil)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var c Comment
	err = tx.QueryRow(ctx, `
		WITH ins AS (
		    INSERT INTO comments (task_id, author_id, body) VALUES ($1, $2, $3)
		    RETURNING id, author_id, body, created_at, edited_at)
		SELECT ins.id, ins.author_id, COALESCE(u.display_name, ''), ins.body, ins.created_at, ins.edited_at
		  FROM ins LEFT JOIN users u ON u.id = ins.author_id`, taskID, actor, body,
	).Scan(&c.ID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.EditedAt)
	if err != nil {
		if strings.Contains(err.Error(), "SQLSTATE 23503") {
			return Comment{}, httperror.NotFound("task")
		}
		return Comment{}, err
	}
	snippet := body
	if len(snippet) > 120 {
		snippet = snippet[:120] + "…"
	}
	if len(mentions) > 0 {
		if err := s.notify(ctx, tx, taskID, actor, "mention", "mentioned you: "+snippet, mentions); err != nil {
			return Comment{}, err
		}
	}
	// Followers who were mentioned already got the louder notification.
	if err := s.notifyFollowersExcept(ctx, tx, taskID, actor, "comment", "commented: "+snippet, mentions); err != nil {
		return Comment{}, err
	}
	// Automations: "when a comment is added…" (TASK-22).
	if err := s.fireAutomations(ctx, tx, taskID, &actor, autoTrigger{kind: "comment_added"}, map[uuid.UUID]bool{}, 0); err != nil {
		return Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	return c, nil
}

// UpdateComment edits a comment's body (author only) and stamps edited_at
// (TASK-47).
func (s *Service) UpdateComment(ctx context.Context, actor, commentID uuid.UUID, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, httperror.Validation("comment is empty", nil)
	}
	if len(body) > maxCommentLen {
		return Comment{}, httperror.Validation("comment is too long", nil)
	}
	var c Comment
	err := s.pool.QueryRow(ctx, `
		WITH upd AS (
		    UPDATE comments SET body = $3, edited_at = now()
		     WHERE id = $1 AND author_id = $2
		    RETURNING id, author_id, body, created_at, edited_at)
		SELECT upd.id, upd.author_id, COALESCE(u.display_name, ''), upd.body, upd.created_at, upd.edited_at
		  FROM upd LEFT JOIN users u ON u.id = upd.author_id`, commentID, actor, body,
	).Scan(&c.ID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.EditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the comment does not exist or it is someone else's; check
		// which so the author gets a useful error.
		var exists bool
		if err2 := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM comments WHERE id = $1)`, commentID).Scan(&exists); err2 == nil && exists {
			return Comment{}, httperror.New(httperror.CodeForbidden, "you can only edit your own comments")
		}
		return Comment{}, httperror.NotFound("comment")
	}
	return c, err
}

// CommentTask returns the task a comment belongs to, so the handler can
// run its access checks before moderating.
func (s *Service) CommentTask(ctx context.Context, commentID uuid.UUID) (uuid.UUID, *uuid.UUID, error) {
	var taskID uuid.UUID
	var author *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT task_id, author_id FROM comments WHERE id = $1`, commentID).Scan(&taskID, &author)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, httperror.NotFound("comment")
	}
	return taskID, author, err
}

// DeleteComment removes a comment; only its author (or a space manager,
// checked by the caller via force) may delete it. The delete is recorded
// in the audit_log (TASK-29).
func (s *Service) DeleteComment(ctx context.Context, actor, commentID uuid.UUID, force bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var author *uuid.UUID
	var taskID uuid.UUID
	var body string
	err = tx.QueryRow(ctx, `SELECT author_id, task_id, body FROM comments WHERE id = $1`, commentID).Scan(&author, &taskID, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("comment")
	}
	if err != nil {
		return err
	}
	if !force && (author == nil || *author != actor) {
		return httperror.New(httperror.CodeForbidden, "you can only delete your own comments")
	}
	if len(body) > 200 {
		body = body[:200]
	}
	if err := writeAudit(ctx, tx, &actor, "comment", commentID, "deleted", map[string]any{"task_id": taskID, "body": body}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM comments WHERE id = $1`, commentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
