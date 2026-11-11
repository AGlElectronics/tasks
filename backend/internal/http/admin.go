package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Admin routes (settings.manage, enforced by the route group).

// audit serves GET /api/v1/audit: the audit log, newest first, with
// optional filters ?entity= ?action= ?actor= ?entity_id= ?since= ?until=
// (RFC 3339 or YYYY-MM-DD) and the usual ?limit= / ?cursor= paging.
func (h *taskHandler) audit(r *http.Request, _ *auth.User) (int, any, error) {
	q := r.URL.Query()
	f := tasks.AuditFilter{Entity: q.Get("entity"), Action: q.Get("action")}
	issues := map[string]string{}
	for name, dst := range map[string]**uuid.UUID{"actor": &f.ActorID, "entity_id": &f.EntityID} {
		if raw := q.Get(name); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				issues[name] = "must be a uuid"
				continue
			}
			*dst = &id
		}
	}
	for name, dst := range map[string]**time.Time{"since": &f.Since, "until": &f.Until} {
		if raw := q.Get(name); raw != "" {
			at, err := parseTime(raw)
			if err != nil {
				issues[name] = "must be RFC 3339 or YYYY-MM-DD"
				continue
			}
			*dst = &at
		}
	}
	if len(issues) > 0 {
		return 0, nil, httperror.Validation("invalid filter", issues)
	}
	limit, cursor := pageParams(r)
	out, next, err := h.svc.AuditLog(r.Context(), f, limit, cursor)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, pagedBody{body: map[string]any{"entries": out}, next: next}, nil
}

func parseTime(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}
