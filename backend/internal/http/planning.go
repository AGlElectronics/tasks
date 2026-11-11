package http

import (
	"net/http"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
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
