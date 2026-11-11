package tasks

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// AuditEntry is one audit_log row as admins read it.
type AuditEntry struct {
	ID        uuid.UUID       `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	ActorID   *uuid.UUID      `json:"actor_id"`
	ActorName string          `json:"actor_name"`
	Entity    string          `json:"entity"`
	EntityID  uuid.UUID       `json:"entity_id"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data"`
}

// AuditFilter narrows the audit feed; zero values do not filter.
type AuditFilter struct {
	Entity   string
	Action   string
	ActorID  *uuid.UUID
	EntityID *uuid.UUID
	Since    *time.Time
	Until    *time.Time
}

// AuditEntities are the values the entity filter accepts.
var AuditEntities = []string{"task", "list", "space", "comment", "attachment", "grant", "settings", "field"}

// AuditLog returns one page of the audit log, newest first, and the cursor
// for the next (older) page.
func (s *Service) AuditLog(ctx context.Context, f AuditFilter, limit int, cursor string) ([]AuditEntry, string, error) {
	limit = ClampLimit(limit)
	if f.Entity != "" {
		known := false
		for _, e := range AuditEntities {
			known = known || e == f.Entity
		}
		if !known {
			return nil, "", httperror.Validation("invalid filter", map[string]string{"entity": "unknown entity"})
		}
	}
	var c timeCursor
	if err := decodeCursor(cursor, &c); err != nil {
		return nil, "", err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.created_at, a.actor_id, COALESCE(u.display_name, CASE WHEN a.actor_id IS NULL THEN 'System' ELSE 'Former user' END),
		       a.entity, a.entity_id, a.action, a.data
		  FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		 WHERE ($1 = '' OR a.entity = $1) AND ($2 = '' OR a.action = $2)
		   AND ($3::uuid IS NULL OR a.actor_id = $3) AND ($4::uuid IS NULL OR a.entity_id = $4)
		   AND ($5::timestamptz IS NULL OR a.created_at >= $5) AND ($6::timestamptz IS NULL OR a.created_at < $6)
		   AND ($7::bool OR (a.created_at, a.id) < ($8, $9))
		 ORDER BY a.created_at DESC, a.id DESC LIMIT $10`,
		f.Entity, f.Action, f.ActorID, f.EntityID, f.Since, f.Until, cursor == "", c.At, c.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var data []byte
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.ActorID, &e.ActorName, &e.Entity, &e.EntityID, &e.Action, &data); err != nil {
			return nil, "", err
		}
		e.Data = data
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
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
