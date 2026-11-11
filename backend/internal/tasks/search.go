package tasks

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// SearchHit is one Ctrl+K result row (TASK-8).
type SearchHit struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	ListName  string    `json:"list_name"`
	SpaceName string    `json:"space_name"`
	Status    string    `json:"status"`
	Done      bool      `json:"done"`
}

// MaxCalendarDays bounds the calendar's date range; MaxCalendarTasks its
// result.
const (
	MaxCalendarDays  = 120
	MaxCalendarTasks = 2000
)

// Calendar returns tasks the caller can see whose start-to-due span
// overlaps [from, to], across all spaces (same visibility as Search). With
// mine set, only tasks assigned to the caller. truncated reports that more
// than MaxCalendarTasks matched.
func (s *Service) Calendar(ctx context.Context, from, to string, userID uuid.UUID, viewSpaceIDs, manageSpaceIDs, listIDs []uuid.UUID, mine bool) ([]Task, bool, error) {
	out, err := s.queryTasks(ctx, `
		WHERE t.deleted_at IS NULL
		  AND COALESCE(t.start_at, t.due_at) IS NOT NULL
		  AND LEAST(COALESCE(t.start_at, t.due_at), COALESCE(t.due_at, t.start_at)) <= $2::date
		  AND GREATEST(COALESCE(t.start_at, t.due_at), COALESCE(t.due_at, t.start_at)) >= $1::date
		  AND ((t.space_id = ANY($3) AND NOT l.private)
		       OR t.space_id = ANY($4)
		       OR t.list_id = ANY($5)
		       OR EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id = t.id AND a.user_id = $6))
		  AND (NOT $7 OR EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id = t.id AND a.user_id = $6))
		ORDER BY COALESCE(t.due_at, t.start_at), t.id
		LIMIT $8`, from, to, viewSpaceIDs, manageSpaceIDs, listIDs, userID, mine, MaxCalendarTasks+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(out) > MaxCalendarTasks
	if truncated {
		out = out[:MaxCalendarTasks]
	}
	return out, truncated, nil
}

// Search finds tasks by title or KEY-N across what the caller can see: the
// handler passes the spaces the caller has at least view on (private lists
// excluded unless managed), plus lists shared directly. Matching is simple
// ILIKE — fine at this scale — and an exact-key match is pinned to the top
// for the "jump to TASK-12" case.
func (s *Service) Search(ctx context.Context, q string, userID uuid.UUID, viewSpaceIDs, manageSpaceIDs, listIDs []uuid.UUID, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, sp.key || '-' || t.number, t.title, l.name, sp.name, st.name, st.category = 'done'
		  FROM tasks t
		  JOIN spaces sp ON sp.id = t.space_id
		  JOIN lists l ON l.id = t.list_id
		  JOIN statuses st ON st.id = t.status_id
		 WHERE t.deleted_at IS NULL
		   AND ((t.space_id = ANY($2) AND NOT l.private)
		        OR t.space_id = ANY($3)
		        OR t.list_id = ANY($4)
		        OR EXISTS (SELECT 1 FROM task_assignees a WHERE a.task_id = t.id AND a.user_id = $5))
		   AND (t.title ILIKE '%' || $1 || '%' OR sp.key || '-' || t.number ILIKE $1 || '%')
		 ORDER BY (sp.key || '-' || t.number = upper($1)) DESC, st.category = 'done', t.updated_at DESC
		 LIMIT $6`, q, viewSpaceIDs, manageSpaceIDs, listIDs, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Key, &h.Title, &h.ListName, &h.SpaceName, &h.Status, &h.Done); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
