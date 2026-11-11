package tasks

import (
	"context"
	"fmt"
	"time"
)

// PortalTodo is one item Den shows on the portal home. The shape is Den's
// contract: id and title are required, url must be absolute http(s) or Den
// drops the item, severity is info, warning or danger, due_at is RFC3339 or
// null.
type PortalTodo struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Detail   string  `json:"detail,omitempty"`
	DueAt    *string `json:"due_at"`
	URL      string  `json:"url"`
	Severity string  `json:"severity"`
}

// portalTodoLimit matches the number of items Den keeps per module.
const portalTodoLimit = 50

// PortalTodos lists open tasks assigned to the person Den names (issuer and
// subject). Done, trashed, and tasks in an archived space or list are left
// out. An unknown person gets an empty list.
func (s *Service) PortalTodos(ctx context.Context, issuer, subject string) ([]PortalTodo, error) {
	soon, err := s.Setting(ctx, "reminders.due_soon_days")
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sp.key || '-' || t.number, t.title, l.name, sp.name,
		       t.due_at, COALESCE(t.due_at < CURRENT_DATE, false),
		       CASE WHEN t.due_at IS NULL THEN NULL ELSE (t.due_at - CURRENT_DATE)::int END
		  FROM tasks t
		  JOIN spaces sp ON sp.id = t.space_id
		  JOIN lists l ON l.id = t.list_id
		  JOIN statuses st ON st.id = t.status_id
		  JOIN task_assignees a ON a.task_id = t.id
		  JOIN users u ON u.id = a.user_id
		 WHERE u.issuer = $1 AND u.subject = $2
		   AND t.deleted_at IS NULL AND st.category <> 'done'
		   AND sp.archived_at IS NULL AND l.archived_at IS NULL
		 ORDER BY t.due_at NULLS LAST, t.updated_at DESC
		 LIMIT $3`, issuer, subject, portalTodoLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortalTodo{}
	for rows.Next() {
		var key, title, listName, spaceName string
		var due *time.Time
		var overdue bool
		var inDays *int
		if err := rows.Scan(&key, &title, &listName, &spaceName, &due, &overdue, &inDays); err != nil {
			return nil, err
		}
		item := PortalTodo{
			ID: key, Title: title, Severity: "info",
			Detail: listName + " · " + spaceName,
		}
		if s.publicURL != "" {
			item.URL = s.publicURL + "/t/" + key
		}
		if due != nil {
			y, m, d := due.Date()
			s := fmt.Sprintf("%04d-%02d-%02dT00:00:00Z", y, int(m), d)
			item.DueAt = &s
		}
		switch {
		case overdue:
			item.Severity = "danger"
		case inDays != nil && soon > 0 && *inDays >= 0 && *inDays <= soon:
			item.Severity = "warning"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
