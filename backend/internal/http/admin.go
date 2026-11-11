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

// getSettings serves GET /api/v1/settings: effective retention and
// reminder settings, where each comes from, and the email status.
func (h *taskHandler) getSettings(r *http.Request, _ *auth.User) (int, any, error) {
	v, err := h.svc.Settings(r.Context())
	return http.StatusOK, v, err
}

// putSettings serves PUT /api/v1/settings with a partial nested body.
func (h *taskHandler) putSettings(r *http.Request, u *auth.User) (int, any, error) {
	var p tasks.SettingsPatch
	if err := decodeJSON(r, &p); err != nil {
		return 0, nil, err
	}
	v, err := h.svc.UpdateSettings(r.Context(), u.ID, p)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, v, nil
}

// testEmail serves POST /api/v1/settings/email/test: one email to the
// calling admin's own address.
func (h *taskHandler) testEmail(r *http.Request, u *auth.User) (int, any, error) {
	if err := h.svc.SendTestEmail(r.Context(), h.send, u.Email); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"sent_to": u.Email}, nil
}

// notificationPrefs serves GET and PUT /api/v1/me/notifications.
func (h *taskHandler) notificationPrefs(r *http.Request, u *auth.User) (int, any, error) {
	if r.Method == http.MethodGet {
		p, err := h.svc.NotificationPreferences(r.Context(), u.ID)
		return http.StatusOK, p, err
	}
	var in tasks.NotificationPrefs
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	p, err := h.svc.SetNotificationPreferences(r.Context(), u.ID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, p, nil
}

func parseTime(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}
