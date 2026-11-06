package tasks

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// ChecklistItem is one line of a task's checklist (TASK-6): lighter than a
// subtask, for the "did we do the five standard things" kind of work.
type ChecklistItem struct {
	ID       uuid.UUID `json:"id"`
	TaskID   uuid.UUID `json:"task_id"`
	Body     string    `json:"body"`
	Done     bool      `json:"done"`
	Position int       `json:"position"`
}

// Checklist lists a task's checklist items in order.
func (s *Service) Checklist(ctx context.Context, taskID uuid.UUID) ([]ChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, body, done, position FROM checklist_items
		 WHERE task_id = $1 ORDER BY position, created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var i ChecklistItem
		if err := rows.Scan(&i.ID, &i.TaskID, &i.Body, &i.Done, &i.Position); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AddChecklistItem appends an item to a task's checklist.
func (s *Service) AddChecklistItem(ctx context.Context, actor, taskID uuid.UUID, body string) (ChecklistItem, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return ChecklistItem{}, httperror.Validation("checklist item is empty", nil)
	}
	if len(body) > maxTitleLen {
		return ChecklistItem{}, httperror.Validation("checklist item is too long", nil)
	}
	i := ChecklistItem{TaskID: taskID, Body: body}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO checklist_items (task_id, body, position)
		VALUES ($1, $2, (SELECT COALESCE(max(position), 0) + 1 FROM checklist_items WHERE task_id = $1))
		RETURNING id, position`, taskID, body).Scan(&i.ID, &i.Position)
	if err != nil && strings.Contains(err.Error(), "SQLSTATE 23503") {
		return ChecklistItem{}, httperror.NotFound("task")
	}
	return i, err
}

// ChecklistPatch updates one checklist item.
type ChecklistPatch struct {
	Body     *string `json:"body"`
	Done     *bool   `json:"done"`
	Position *int    `json:"position"`
}

// ChecklistItemTask returns which task an item belongs to (for access
// checks in the handler).
func (s *Service) ChecklistItemTask(ctx context.Context, itemID uuid.UUID) (uuid.UUID, error) {
	var taskID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT task_id FROM checklist_items WHERE id = $1`, itemID).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.NotFound("checklist item")
	}
	return taskID, err
}

// UpdateChecklistItem applies the non-nil fields. Ticking the last open
// item logs a "checklist completed" event (also an automation trigger).
func (s *Service) UpdateChecklistItem(ctx context.Context, actor, itemID uuid.UUID, p ChecklistPatch) (ChecklistItem, error) {
	if p.Body != nil {
		b := strings.TrimSpace(*p.Body)
		if b == "" {
			return ChecklistItem{}, httperror.Validation("checklist item is empty", nil)
		}
		if len(b) > maxTitleLen {
			return ChecklistItem{}, httperror.Validation("checklist item is too long", nil)
		}
		p.Body = &b
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChecklistItem{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var i ChecklistItem
	var wasDone bool
	err = tx.QueryRow(ctx, `SELECT id, task_id, body, done, position FROM checklist_items WHERE id = $1 FOR UPDATE`, itemID).
		Scan(&i.ID, &i.TaskID, &i.Body, &wasDone, &i.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChecklistItem{}, httperror.NotFound("checklist item")
	}
	if err != nil {
		return ChecklistItem{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE checklist_items SET body = COALESCE($2, body), done = COALESCE($3, done), position = COALESCE($4, position)
		 WHERE id = $1 RETURNING body, done, position`, itemID, p.Body, p.Done, p.Position).Scan(&i.Body, &i.Done, &i.Position)
	if err != nil {
		return ChecklistItem{}, err
	}
	if p.Done != nil && *p.Done && !wasDone {
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM checklist_items WHERE task_id = $1 AND NOT done`, i.TaskID).Scan(&open); err != nil {
			return ChecklistItem{}, err
		}
		if open == 0 {
			if err := addEvent(ctx, tx, i.TaskID, actor, "checklist_completed", nil); err != nil {
				return ChecklistItem{}, err
			}
		}
	}
	return i, tx.Commit(ctx)
}

// DeleteChecklistItem removes one item.
func (s *Service) DeleteChecklistItem(ctx context.Context, itemID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM checklist_items WHERE id = $1`, itemID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("checklist item")
	}
	return nil
}
