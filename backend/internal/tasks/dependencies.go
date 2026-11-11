package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// ----------------------------------------------------------------------------
// Dependencies: "this task is blocked by that one".
// ----------------------------------------------------------------------------

// DepRef is the other end of a dependency as the task page shows it.
type DepRef struct {
	ID      uuid.UUID  `json:"id"`
	Key     string     `json:"key"`
	Title   string     `json:"title"`
	Status  TaskStatus `json:"status"`
	Done    bool       `json:"done"`
	ListID  uuid.UUID  `json:"list_id"`
	SpaceID uuid.UUID  `json:"space_id"`
}

const maxDependencies = 50

const depSelect = `
	SELECT t.id, sp.key || '-' || t.number, t.title, st.id, st.name, st.color, st.category, st.category = 'done', t.list_id, t.space_id
	  FROM tasks t JOIN spaces sp ON sp.id = t.space_id JOIN statuses st ON st.id = t.status_id`

func (s *Service) scanDeps(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, args ...any) ([]DepRef, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DepRef{}
	for rows.Next() {
		var d DepRef
		if err := rows.Scan(&d.ID, &d.Key, &d.Title, &d.Status.ID, &d.Status.Name, &d.Status.Color, &d.Status.Category, &d.Done, &d.ListID, &d.SpaceID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Dependencies returns what blocks the task and what the task blocks
// (trashed tasks left out).
func (s *Service) Dependencies(ctx context.Context, taskID uuid.UUID) (blockedBy, blocking []DepRef, err error) {
	blockedBy, err = s.scanDeps(ctx, s.pool, depSelect+`
		JOIN task_dependencies d ON d.depends_on = t.id
		WHERE d.task_id = $1 AND t.deleted_at IS NULL ORDER BY st.category = 'done', d.created_at`, taskID)
	if err != nil {
		return nil, nil, err
	}
	blocking, err = s.scanDeps(ctx, s.pool, depSelect+`
		JOIN task_dependencies d ON d.task_id = t.id
		WHERE d.depends_on = $1 AND t.deleted_at IS NULL ORDER BY st.category = 'done', d.created_at`, taskID)
	return blockedBy, blocking, err
}

// AddDependency records that taskID is blocked by dependsOn. Adding an
// existing dependency is a no-op; one that would close a cycle is refused.
func (s *Service) AddDependency(ctx context.Context, actor, taskID, dependsOn uuid.UUID) (DepRef, error) {
	if taskID == dependsOn {
		return DepRef{}, httperror.Validation("a task cannot depend on itself", nil)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DepRef{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialise dependency changes so two concurrent adds cannot build a
	// cycle between them.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('task_dependencies'))`); err != nil {
		return DepRef{}, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM task_dependencies WHERE task_id = $1`, taskID).Scan(&count); err != nil {
		return DepRef{}, err
	}
	if count >= maxDependencies {
		return DepRef{}, httperror.Validation(fmt.Sprintf("a task can wait for at most %d others", maxDependencies), nil)
	}
	var cycle bool
	if err := tx.QueryRow(ctx, `
		WITH RECURSIVE chain(id) AS (
		    SELECT $1::uuid
		    UNION
		    SELECT d.depends_on FROM task_dependencies d JOIN chain c ON d.task_id = c.id)
		SELECT EXISTS (SELECT 1 FROM chain WHERE id = $2)`, dependsOn, taskID).Scan(&cycle); err != nil {
		return DepRef{}, err
	}
	if cycle {
		return DepRef{}, httperror.Validation("that would make the tasks wait for each other in a circle", nil)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO task_dependencies (task_id, depends_on, created_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		taskID, dependsOn, actor)
	if err != nil {
		return DepRef{}, err
	}
	refs, err := s.scanDeps(ctx, tx, depSelect+` WHERE t.id = ANY($1)`, []uuid.UUID{taskID, dependsOn})
	if err != nil {
		return DepRef{}, err
	}
	var self, other DepRef
	for _, r := range refs {
		if r.ID == dependsOn {
			other = r
		} else {
			self = r
		}
	}
	if tag.RowsAffected() > 0 {
		if err := addEvent(ctx, tx, taskID, actor, "dependency_added", map[string]any{"blocked_by": other.Key}); err != nil {
			return DepRef{}, err
		}
		if err := addEvent(ctx, tx, dependsOn, actor, "dependency_added", map[string]any{"blocking": self.Key}); err != nil {
			return DepRef{}, err
		}
	}
	return other, tx.Commit(ctx)
}

// RemoveDependency deletes one dependency.
func (s *Service) RemoveDependency(ctx context.Context, actor, taskID, dependsOn uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on = $2`, taskID, dependsOn)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("dependency")
	}
	refs, err := s.scanDeps(ctx, tx, depSelect+` WHERE t.id = ANY($1)`, []uuid.UUID{taskID, dependsOn})
	if err != nil {
		return err
	}
	keys := map[uuid.UUID]string{}
	for _, r := range refs {
		keys[r.ID] = r.Key
	}
	if err := addEvent(ctx, tx, taskID, actor, "dependency_removed", map[string]any{"blocked_by": keys[dependsOn]}); err != nil {
		return err
	}
	if err := addEvent(ctx, tx, dependsOn, actor, "dependency_removed", map[string]any{"blocking": keys[taskID]}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ----------------------------------------------------------------------------
// Recurring tasks
// ----------------------------------------------------------------------------

// Recurrence repeats a task: when it is completed, the next one is created
// with its start and due dates moved on by Interval × Freq.
type Recurrence struct {
	Freq     string `json:"freq"`     // daily, weekly, monthly, yearly
	Interval int    `json:"interval"` // 1..365
}

func (r Recurrence) validate() error {
	switch r.Freq {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return httperror.Validation("invalid recurrence", map[string]string{"recurrence.freq": "must be daily, weekly, monthly or yearly"})
	}
	if r.Interval < 1 || r.Interval > 365 {
		return httperror.Validation("invalid recurrence", map[string]string{"recurrence.interval": "must be 1 to 365"})
	}
	return nil
}

// Describe is "every 2 weeks".
func (r Recurrence) Describe() string {
	unit := map[string]string{"daily": "day", "weekly": "week", "monthly": "month", "yearly": "year"}[r.Freq]
	if r.Interval == 1 {
		return "every " + unit
	}
	return fmt.Sprintf("every %d %ss", r.Interval, unit)
}

// NullableRecurrence is a PATCH field: absent keeps, null stops repeating.
type NullableRecurrence struct {
	Set   bool
	Value *Recurrence
}

// UnmarshalJSON records that the field was present.
func (n *NullableRecurrence) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	var r Recurrence
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	n.Value = &r
	return nil
}

// spawnRecurrence creates the next occurrence of a recurring top-level task
// that was just completed, inside the caller's transaction. The copy keeps
// title, description, list, priority, assignees, watchers, tags, custom
// field values and checklist (unchecked), starts in the space's
// first not-started status, and takes over the recurrence rule. Subtasks,
// comments and attachments are not copied.
func (s *Service) spawnRecurrence(ctx context.Context, tx pgx.Tx, id uuid.UUID, actor *uuid.UUID) error {
	var raw []byte
	var spaceID, listID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT recurrence, space_id, list_id FROM tasks WHERE id = $1 AND parent_id IS NULL AND recurrence IS NOT NULL AND deleted_at IS NULL`, id).
		Scan(&raw, &spaceID, &listID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var rec Recurrence
	if json.Unmarshal(raw, &rec) != nil || rec.validate() != nil {
		return nil
	}
	n := rec.Interval
	step := map[string]string{
		"daily":   "make_interval(days => $5)",
		"weekly":  "make_interval(weeks => $5)",
		"monthly": "make_interval(months => $5)",
		"yearly":  "make_interval(years => $5)",
	}[rec.Freq]
	var statusID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM statuses WHERE space_id = $1 ORDER BY category <> 'not_started', position LIMIT 1`, spaceID).Scan(&statusID); err != nil {
		return err
	}
	var number int
	if err := tx.QueryRow(ctx, `UPDATE spaces SET next_number = next_number + 1 WHERE id = $1 RETURNING next_number - 1`, spaceID).Scan(&number); err != nil {
		return err
	}
	var nextID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO tasks (space_id, list_id, number, title, description, status_id, priority, start_at, due_at,
		                   position, created_by, recurrence)
		SELECT space_id, list_id, $2, title, description, $3, priority,
		       (start_at + `+step+`)::date, (due_at + `+step+`)::date,
		       (SELECT COALESCE(max(position), 0) + 1024 FROM tasks WHERE list_id = t.list_id), $4, recurrence
		  FROM tasks t WHERE id = $1
		RETURNING id`, id, number, statusID, actor, n).Scan(&nextID); err != nil {
		return err
	}
	for _, q := range []string{
		`INSERT INTO task_assignees (task_id, user_id) SELECT $2, user_id FROM task_assignees WHERE task_id = $1`,
		`INSERT INTO task_watchers (task_id, user_id) SELECT $2, user_id FROM task_watchers WHERE task_id = $1`,
		`INSERT INTO task_tags (task_id, tag_id) SELECT $2, tag_id FROM task_tags WHERE task_id = $1`,
		`INSERT INTO task_field_values (task_id, field_id, value) SELECT $2, field_id, value FROM task_field_values WHERE task_id = $1`,
		`INSERT INTO checklist_items (task_id, body, position) SELECT $2, body, position FROM checklist_items WHERE task_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, id, nextID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE tasks SET recurrence = NULL WHERE id = $1`, id); err != nil {
		return err
	}
	var oldKey, newKey string
	if err := tx.QueryRow(ctx, `
		SELECT sp.key || '-' || o.number, sp.key || '-' || n.number
		  FROM tasks o JOIN tasks n ON n.id = $2 JOIN spaces sp ON sp.id = n.space_id WHERE o.id = $1`, id, nextID).Scan(&oldKey, &newKey); err != nil {
		return err
	}
	if err := autoEvent(ctx, tx, id, actor, "recurred", map[string]any{"next": newKey, "rule": rec.Describe()}); err != nil {
		return err
	}
	return autoEvent(ctx, tx, nextID, actor, "created", map[string]any{"from": oldKey, "rule": rec.Describe()})
}
