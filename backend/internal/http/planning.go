package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Planning features: custom fields, dependencies, time tracking.

// ----------------------------------------------------------------------------
// Dependencies: edit on the waiting task, view on the task it waits for.
// ----------------------------------------------------------------------------

func (h *taskHandler) addDependency(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	var in struct {
		DependsOn string `json:"depends_on"` // uuid or KEY-N
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	other, err := h.svc.ResolveTask(r.Context(), in.DependsOn)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, other, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	ref, err := h.svc.AddDependency(r.Context(), u.ID, id, other)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, ref, nil
}

func (h *taskHandler) removeDependency(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	other, err := h.svc.ResolveTask(r.Context(), chiParam(r, "other"))
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.RemoveDependency(r.Context(), u.ID, id, other)
}

// ----------------------------------------------------------------------------
// Time tracking: view to read, edit to log; an entry is removed by the
// person who logged it or a space manager.
// ----------------------------------------------------------------------------

func (h *taskHandler) listTime(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	t, _, err := h.requireTask(r, u, id, tasks.LevelView)
	if err != nil {
		return 0, nil, err
	}
	out, err := h.svc.TimeEntries(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"entries": out, "total_minutes": t.TimeSpentMinutes, "estimate_minutes": t.EstimateMinutes}, nil
}

func (h *taskHandler) logTime(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	var in tasks.TimeInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	e, err := h.svc.LogTime(r.Context(), u.ID, id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, e, nil
}

func (h *taskHandler) deleteTime(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "entry")
	if err != nil {
		return 0, nil, err
	}
	e, err := h.svc.GetTimeEntry(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	_, level, err := h.requireTask(r, u, e.TaskID, tasks.LevelEdit)
	if err != nil {
		return 0, nil, err
	}
	own := e.UserID != nil && *e.UserID == u.ID
	if !own && level < tasks.LevelManage {
		return 0, nil, httperror.New(httperror.CodeForbidden, "only the person who logged the time or a space manager can remove it")
	}
	return http.StatusNoContent, nil, h.svc.DeleteTimeEntry(r.Context(), u.ID, e)
}

// listTimeReport serves GET /lists/{list}/time-report?from=&to=.
func (h *taskHandler) listTimeReport(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return 0, nil, err
	}
	if _, _, _, err := h.requireList(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	var from, to *string
	issues := map[string]string{}
	for name, dst := range map[string]**string{"from": &from, "to": &to} {
		if v := r.URL.Query().Get(name); v != "" {
			if _, err := parseTime(v); err != nil || len(v) != 10 {
				issues[name] = "must be YYYY-MM-DD"
				continue
			}
			*dst = &v
		}
	}
	if len(issues) > 0 {
		return 0, nil, httperror.Validation("invalid report range", issues)
	}
	rows, total, err := h.svc.ListTimeReport(r.Context(), id, from, to)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"people": rows, "total_minutes": total}, nil
}

// calendar serves GET /api/v1/calendar?from=&to=[&mine=1]: dated tasks
// across every space the caller can see, for the calendar page.
func (h *taskHandler) calendar(r *http.Request, u *auth.User) (int, any, error) {
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	issues := map[string]string{}
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil {
		issues["from"] = "required, YYYY-MM-DD"
	}
	if err2 != nil {
		issues["to"] = "required, YYYY-MM-DD"
	}
	if err1 == nil && err2 == nil && (t.Before(f) || t.Sub(f) > tasks.MaxCalendarDays*24*time.Hour) {
		issues["to"] = fmt.Sprintf("must be on or after from and at most %d days later", tasks.MaxCalendarDays)
	}
	if len(issues) > 0 {
		return 0, nil, httperror.Validation("invalid range", issues)
	}
	vis, err := h.visibility(r.Context(), u, false)
	if err != nil {
		return 0, nil, err
	}
	out, truncated, err := h.svc.Calendar(r.Context(), from, to, u.ID, vis.viewSpaces, vis.manageSpaces, vis.grantedLists, q.Get("mine") == "1")
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"tasks": out, "truncated": truncated}, nil
}

// visibleDeps drops the dependency ends the caller cannot see.
func (h *taskHandler) visibleDeps(r *http.Request, u *auth.User, refs []tasks.DepRef) ([]tasks.DepRef, int) {
	out := make([]tasks.DepRef, 0, len(refs))
	hidden := 0
	for _, d := range refs {
		if _, _, err := h.requireTask(r, u, d.ID, tasks.LevelView); err != nil {
			hidden++
			continue
		}
		out = append(out, d)
	}
	return out, hidden
}

// ----------------------------------------------------------------------------
// Custom fields: listing needs view on the space, changing needs manage.
// Values are set per task through PATCH /tasks/{task} {"fields": {...}}.
// ----------------------------------------------------------------------------

func (h *taskHandler) listFields(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.SpaceFields(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"fields": out}, nil
}

func (h *taskHandler) createField(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.FieldInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	f, err := h.svc.CreateField(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, f, nil
}

// requireField loads a field and checks manage on its space.
func (h *taskHandler) requireField(r *http.Request, u *auth.User) (tasks.Field, error) {
	id, err := parseUUIDParam(r, "field")
	if err != nil {
		return tasks.Field{}, err
	}
	f, err := h.svc.GetField(r.Context(), id)
	if err != nil {
		return tasks.Field{}, err
	}
	if _, _, err := h.requireSpace(r, u, f.SpaceID, tasks.LevelManage); err != nil {
		return tasks.Field{}, err
	}
	return f, nil
}

func (h *taskHandler) updateField(r *http.Request, u *auth.User) (int, any, error) {
	f, err := h.requireField(r, u)
	if err != nil {
		return 0, nil, err
	}
	var in tasks.FieldInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.UpdateField(r.Context(), f.ID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, out, nil
}

func (h *taskHandler) deleteField(r *http.Request, u *auth.User) (int, any, error) {
	f, err := h.requireField(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteField(r.Context(), u.ID, f.ID)
}
