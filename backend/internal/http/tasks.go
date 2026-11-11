package http

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/files"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/realtime"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// taskHandler owns space, list, task and comment routes.
//
// Authorization model (TASK-1, TASK-27): access is a level — view, edit or
// manage — computed per request from, strongest wins:
//
//   - space.manage in the space's organization (org admins see everything);
//   - space.create plus being the space's creator;
//   - the space's everyone_level (nil = closed space);
//   - a space grant for the user or one of their IdP groups;
//   - a list grant likewise (cross-team sharing without the whole space);
//   - being assigned to the task (opens that one task at edit level).
//
// Private lists hide from space-level viewers; only list grants and space
// managers see them. Reads below view return 404 (no existence leak);
// writes below the required level return 403.
type taskHandler struct {
	svc   *tasks.Service
	files *files.Client // nil = attachments not configured
	send  tasks.SendFunc
	// hub feeds the live-update stream; nil = live updates off.
	hub     *realtime.Hub
	streams streamLimiter
}

func caller(r *http.Request) (*auth.User, error) {
	u, err := auth.MustFromContext(r.Context())
	if err != nil {
		return nil, httperror.ErrUnauthorized
	}
	return u, nil
}

// pagedBody wraps a response body and the next-page cursor; handle() turns
// the cursor into the X-Next-Cursor header so array bodies keep their shape
// (TASK-38).
type pagedBody struct {
	body any
	next string
}

// handle adapts a handler that returns (status, body, error).
func handle(fn func(r *http.Request, u *auth.User) (int, any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := caller(r)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		status, body, err := fn(r, u)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		if pb, ok := body.(pagedBody); ok {
			if pb.next != "" {
				w.Header().Set("X-Next-Cursor", pb.next)
			}
			body = pb.body
		}
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, body)
	}
}

// pageParams reads ?limit= and ?cursor= (TASK-38).
func pageParams(r *http.Request) (int, string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return tasks.ClampLimit(limit), r.URL.Query().Get("cursor")
}

// ----------------------------------------------------------------------------
// Access levels
// ----------------------------------------------------------------------------

// levelForSpace is the caller's level on a space, org overrides included.
func (h *taskHandler) levelForSpace(r *http.Request, u *auth.User, sp tasks.Space) (tasks.Level, error) {
	if u.HasInOrg(domain.PermSpaceManage, sp.OrgID) {
		return tasks.LevelManage, nil
	}
	if u.HasInOrg(domain.PermSpaceCreate, sp.OrgID) && sp.CreatedBy != nil && *sp.CreatedBy == u.ID {
		return tasks.LevelManage, nil
	}
	return h.svc.SpaceGrantLevel(r.Context(), sp, u.ID, u.Groups)
}

// levelForList is the caller's level on a list: the space level (dropped to
// none on private lists unless managing) raised by any direct list grant.
func (h *taskHandler) levelForList(r *http.Request, u *auth.User, l tasks.List, sp tasks.Space) (tasks.Level, error) {
	spLevel, err := h.levelForSpace(r, u, sp)
	if err != nil {
		return tasks.LevelNone, err
	}
	if l.Private && spLevel < tasks.LevelManage {
		spLevel = tasks.LevelNone
	}
	listLevel, err := h.svc.ListGrantLevel(r.Context(), l.ID, u.ID, u.Groups)
	if err != nil {
		return tasks.LevelNone, err
	}
	return spLevel.Max(listLevel), nil
}

// requireSpace loads a space and enforces a minimum level. Below view the
// space 404s; below the required level the caller gets 403.
func (h *taskHandler) requireSpace(r *http.Request, u *auth.User, spaceID uuid.UUID, min tasks.Level) (tasks.Space, tasks.Level, error) {
	sp, err := h.svc.GetSpace(r.Context(), spaceID)
	if err != nil {
		return sp, tasks.LevelNone, err
	}
	level, err := h.levelForSpace(r, u, sp)
	if err != nil {
		return sp, level, err
	}
	if level < min {
		if level == tasks.LevelNone {
			return sp, level, httperror.NotFound("space")
		}
		return sp, level, httperror.ErrForbidden
	}
	return sp, level, nil
}

// requireList loads a list (with its space) and enforces a minimum level.
func (h *taskHandler) requireList(r *http.Request, u *auth.User, listID uuid.UUID, min tasks.Level) (tasks.List, tasks.Space, tasks.Level, error) {
	l, err := h.svc.GetList(r.Context(), listID)
	if err != nil {
		return l, tasks.Space{}, tasks.LevelNone, err
	}
	sp, err := h.svc.GetSpace(r.Context(), l.SpaceID)
	if err != nil {
		return l, sp, tasks.LevelNone, err
	}
	level, err := h.levelForList(r, u, l, sp)
	if err != nil {
		return l, sp, level, err
	}
	if level < min {
		if level == tasks.LevelNone {
			return l, sp, level, httperror.NotFound("list")
		}
		return l, sp, level, httperror.ErrForbidden
	}
	return l, sp, level, nil
}

// requireTask loads a task and enforces a minimum level. Being assigned to
// the task grants edit on that one task, whatever the space says.
func (h *taskHandler) requireTask(r *http.Request, u *auth.User, taskID uuid.UUID, min tasks.Level) (tasks.Task, tasks.Level, error) {
	t, err := h.svc.GetTask(r.Context(), taskID)
	if err != nil {
		return t, tasks.LevelNone, err
	}
	l, err := h.svc.GetList(r.Context(), t.ListID)
	if err != nil {
		return t, tasks.LevelNone, err
	}
	sp, err := h.svc.GetSpace(r.Context(), l.SpaceID)
	if err != nil {
		return t, tasks.LevelNone, err
	}
	level, err := h.levelForList(r, u, l, sp)
	if err != nil {
		return t, level, err
	}
	if level < tasks.LevelEdit {
		assigned, err := h.svc.IsAssigned(r.Context(), t.ID, u.ID)
		if err != nil {
			return t, level, err
		}
		if assigned {
			level = tasks.LevelEdit
		}
	}
	// Watching opens that one task read-only, like assignment opens it
	// for editing — notifications must stay clickable (TASK-5).
	if level < tasks.LevelView {
		watching, err := h.svc.IsWatching(r.Context(), u.ID, t.ID)
		if err != nil {
			return t, level, err
		}
		if watching {
			level = tasks.LevelView
		}
	}
	if level < min {
		if level == tasks.LevelNone {
			return t, level, httperror.NotFound("task")
		}
		return t, level, httperror.ErrForbidden
	}
	return t, level, nil
}

// spaceView adds the caller's capability flags to a space.
type spaceView struct {
	tasks.Space
	MyLevel   string `json:"my_level"`
	CanEdit   bool   `json:"can_edit"`
	CanManage bool   `json:"can_manage"`
	// SharedOnly: the caller has no space access; only the shared lists
	// included here are visible.
	SharedOnly bool `json:"shared_only,omitempty"`
}

func view(sp tasks.Space, level tasks.Level) spaceView {
	return spaceView{
		Space:     sp,
		MyLevel:   level.String(),
		CanEdit:   level >= tasks.LevelEdit,
		CanManage: level >= tasks.LevelManage,
	}
}

// ----------------------------------------------------------------------------
// Spaces, statuses, lists
// ----------------------------------------------------------------------------

// listSpaces shapes the sidebar per caller: spaces they can enter, plus
// spaces that only show through lists shared with them. Private lists only
// appear for managers and the directly granted.
func (h *taskHandler) listSpaces(r *http.Request, u *auth.User) (int, any, error) {
	spaces, truncated, err := h.svc.ListSpaces(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		return 0, nil, err
	}
	acc, err := h.svc.AccessFor(r.Context(), u.ID, u.Groups)
	if err != nil {
		return 0, nil, err
	}
	out := make([]spaceView, 0, len(spaces))
	for _, sp := range spaces {
		level := tasks.LevelNone
		if sp.EveryoneLevel != nil {
			level = tasks.ParseLevel(*sp.EveryoneLevel)
		}
		level = level.Max(acc.SpaceLevels[sp.ID])
		if u.HasInOrg(domain.PermSpaceManage, sp.OrgID) ||
			(u.HasInOrg(domain.PermSpaceCreate, sp.OrgID) && sp.CreatedBy != nil && *sp.CreatedBy == u.ID) {
			level = tasks.LevelManage
		}

		if level >= tasks.LevelView {
			if level < tasks.LevelManage {
				visible := make([]tasks.List, 0, len(sp.Lists))
				for _, l := range sp.Lists {
					if !l.Private || acc.ListLevels[l.ID] >= tasks.LevelView {
						visible = append(visible, l)
					}
				}
				sp.Lists = visible
			}
			out = append(out, view(sp, level))
			continue
		}

		// No space access: show only lists shared directly with the caller.
		shared := make([]tasks.List, 0, 1)
		for _, l := range sp.Lists {
			if acc.ListLevels[l.ID] >= tasks.LevelView {
				shared = append(shared, l)
			}
		}
		if len(shared) > 0 {
			sp.Lists = shared
			v := view(sp, tasks.LevelNone)
			v.SharedOnly = true
			out = append(out, v)
		}
	}
	return http.StatusOK, map[string]any{"spaces": out, "truncated": truncated}, nil
}

func (h *taskHandler) createSpace(r *http.Request, u *auth.User) (int, any, error) {
	orgID, err := h.svc.DefaultOrganization(r.Context())
	if err != nil {
		return 0, nil, err
	}
	// Scoped to the organization the space will belong to (TASK-31):
	// HasAny would accept a grant from any organization.
	if !u.HasInOrg(domain.PermSpaceCreate, orgID) && !u.HasInOrg(domain.PermSpaceManage, orgID) {
		return 0, nil, httperror.ErrForbidden
	}
	var in tasks.SpaceInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	sp, err := h.svc.CreateSpace(r.Context(), orgID, u.ID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, view(sp, tasks.LevelManage), nil
}

func (h *taskHandler) getSpace(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	sp, err := h.svc.GetSpace(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	level, err := h.levelForSpace(r, u, sp)
	if err != nil {
		return 0, nil, err
	}
	if level >= tasks.LevelView {
		if level < tasks.LevelManage {
			acc, err := h.svc.AccessFor(r.Context(), u.ID, u.Groups)
			if err != nil {
				return 0, nil, err
			}
			visible := make([]tasks.List, 0, len(sp.Lists))
			for _, l := range sp.Lists {
				if !l.Private || acc.ListLevels[l.ID] >= tasks.LevelView {
					visible = append(visible, l)
				}
			}
			sp.Lists = visible
		}
		return http.StatusOK, view(sp, level), nil
	}
	// Space closed to the caller: still reachable when lists are shared.
	acc, err := h.svc.AccessFor(r.Context(), u.ID, u.Groups)
	if err != nil {
		return 0, nil, err
	}
	shared := make([]tasks.List, 0, 1)
	for _, l := range sp.Lists {
		if acc.ListLevels[l.ID] >= tasks.LevelView {
			shared = append(shared, l)
		}
	}
	if len(shared) == 0 {
		return 0, nil, httperror.NotFound("space")
	}
	sp.Lists = shared
	v := view(sp, tasks.LevelNone)
	v.SharedOnly = true
	return http.StatusOK, v, nil
}

func (h *taskHandler) updateSpace(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.SpaceInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	sp, err := h.svc.UpdateSpace(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, view(sp, tasks.LevelManage), nil
}

func (h *taskHandler) deleteSpace(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteSpace(r.Context(), u.ID, id)
}

func (h *taskHandler) replaceStatuses(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in struct {
		Statuses []tasks.StatusInput `json:"statuses"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.ReplaceStatuses(r.Context(), u.ID, id, in.Statuses)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"statuses": out}, nil
}

func (h *taskHandler) createList(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.ListInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	l, err := h.svc.CreateList(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, l, nil
}

// listView is a list with its space (statuses drive the board columns).
type listView struct {
	tasks.List
	Space   spaceView `json:"space"`
	MyLevel string    `json:"my_level"`
	CanEdit bool      `json:"can_edit"`
}

func (h *taskHandler) getList(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return 0, nil, err
	}
	l, sp, level, err := h.requireList(r, u, id, tasks.LevelView)
	if err != nil {
		return 0, nil, err
	}
	spLevel, err := h.levelForSpace(r, u, sp)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, listView{
		List:    l,
		Space:   view(sp, spLevel),
		MyLevel: level.String(),
		CanEdit: level >= tasks.LevelEdit,
	}, nil
}

func (h *taskHandler) updateList(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return 0, nil, err
	}
	if _, _, _, err := h.requireList(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.ListInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	l, err := h.svc.UpdateList(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, l, nil
}

func (h *taskHandler) deleteList(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return 0, nil, err
	}
	if _, _, _, err := h.requireList(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteList(r.Context(), u.ID, id)
}

// ----------------------------------------------------------------------------
// Grants (sharing)
// ----------------------------------------------------------------------------

func (h *taskHandler) grantScope(r *http.Request, u *auth.User) (tasks.GrantScope, error) {
	if raw := chiParam(r, "space"); raw != "" {
		id, err := parseUUIDParam(r, "space")
		if err != nil {
			return tasks.GrantScope{}, err
		}
		if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
			return tasks.GrantScope{}, err
		}
		return tasks.GrantScope{SpaceID: &id}, nil
	}
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return tasks.GrantScope{}, err
	}
	if _, _, _, err := h.requireList(r, u, id, tasks.LevelManage); err != nil {
		return tasks.GrantScope{}, err
	}
	return tasks.GrantScope{ListID: &id}, nil
}

func (h *taskHandler) listGrants(r *http.Request, u *auth.User) (int, any, error) {
	scope, err := h.grantScope(r, u)
	if err != nil {
		return 0, nil, err
	}
	out, err := h.svc.ListGrants(r.Context(), scope)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"grants": out}, nil
}

func (h *taskHandler) putGrant(r *http.Request, u *auth.User) (int, any, error) {
	scope, err := h.grantScope(r, u)
	if err != nil {
		return 0, nil, err
	}
	var in tasks.GrantInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	g, err := h.svc.PutGrant(r.Context(), u.ID, scope, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, g, nil
}

func (h *taskHandler) deleteGrant(r *http.Request, u *auth.User) (int, any, error) {
	scope, err := h.grantScope(r, u)
	if err != nil {
		return 0, nil, err
	}
	id, err := parseUUIDParam(r, "grant")
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteGrant(r.Context(), u.ID, id, scope)
}

// ----------------------------------------------------------------------------
// Tasks
// ----------------------------------------------------------------------------

func (h *taskHandler) listTasks(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "list")
	if err != nil {
		return 0, nil, err
	}
	if _, _, _, err := h.requireList(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	limit, cursor := pageParams(r)
	out, next, err := h.svc.ListTasks(r.Context(), id, limit, cursor)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, pagedBody{body: map[string]any{"tasks": out}, next: next}, nil
}

func (h *taskHandler) myWork(r *http.Request, u *auth.User) (int, any, error) {
	limit, cursor := pageParams(r)
	out, next, err := h.svc.MyWork(r.Context(), u.ID, limit, cursor)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, pagedBody{body: map[string]any{"tasks": out}, next: next}, nil
}

func (h *taskHandler) createTask(r *http.Request, u *auth.User) (int, any, error) {
	var in tasks.TaskInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	// Edit access on the target list (or the parent's list for subtasks).
	switch {
	case in.ParentID != nil:
		if _, _, err := h.requireTask(r, u, *in.ParentID, tasks.LevelEdit); err != nil {
			return 0, nil, err
		}
	case in.ListID != nil:
		if _, _, _, err := h.requireList(r, u, *in.ListID, tasks.LevelEdit); err != nil {
			return 0, nil, err
		}
	default:
		return 0, nil, httperror.Validation("invalid task", map[string]string{"list_id": "required"})
	}
	t, err := h.svc.CreateTask(r.Context(), u.ID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, t, nil
}

func (h *taskHandler) taskID(r *http.Request) (uuid.UUID, error) {
	return h.svc.ResolveTask(r.Context(), chiParam(r, "task"))
}

func (h *taskHandler) getTask(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	d, err := h.svc.Detail(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	var hiddenA, hiddenB int
	d.BlockedBy, hiddenA = h.visibleDeps(r, u, d.BlockedBy)
	d.Blocking, hiddenB = h.visibleDeps(r, u, d.Blocking)
	d.HiddenDependencies = hiddenA + hiddenB
	return http.StatusOK, d, nil
}

// taskComments and taskEvents page through everything the task detail
// leaves out (it embeds only the newest comments and events).
func (h *taskHandler) taskComments(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	limit, cursor := pageParams(r)
	out, next, err := h.svc.Comments(r.Context(), id, limit, cursor)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, pagedBody{body: map[string]any{"comments": out}, next: next}, nil
}

func (h *taskHandler) taskEvents(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	limit, cursor := pageParams(r)
	out, next, err := h.svc.Events(r.Context(), id, limit, cursor)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, pagedBody{body: map[string]any{"events": out}, next: next}, nil
}

func (h *taskHandler) updateTask(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	t, _, err := h.requireTask(r, u, id, tasks.LevelEdit)
	if err != nil {
		return 0, nil, err
	}
	var p tasks.TaskPatch
	if err := decodeJSON(r, &p); err != nil {
		return 0, nil, err
	}
	// Moving to another list (possibly in another space, TASK-10) needs
	// edit rights on the target list too.
	if p.ListID != nil && *p.ListID != t.ListID {
		if _, _, _, err := h.requireList(r, u, *p.ListID, tasks.LevelEdit); err != nil {
			return 0, nil, err
		}
	}
	out, err := h.svc.UpdateTask(r.Context(), u.ID, id, p)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, out, nil
}

// deleteTask: only the task's creator or someone managing the list's space
// may delete (TASK-28).
func (h *taskHandler) deleteTask(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	t, level, err := h.requireTask(r, u, id, tasks.LevelView)
	if err != nil {
		return 0, nil, err
	}
	isCreator := t.CreatedBy != nil && *t.CreatedBy == u.ID
	if !isCreator && level < tasks.LevelManage {
		return 0, nil, httperror.New(httperror.CodeForbidden, "only the task's creator or a space manager can delete it")
	}
	return http.StatusNoContent, nil, h.svc.DeleteTask(r.Context(), u.ID, id)
}

func (h *taskHandler) addComment(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	var in struct {
		Body     string      `json:"body"`
		Mentions []uuid.UUID `json:"mentions"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	c, err := h.svc.AddComment(r.Context(), u.ID, id, in.Body, in.Mentions)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, c, nil
}

// deleteComment: the author may delete their own comment; moderation
// (force) needs manage on the comment's space, resolved through the
// space's own organization and creator — not a global permission (TASK-30).
func (h *taskHandler) deleteComment(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "comment")
	if err != nil {
		return 0, nil, err
	}
	taskID, _, err := h.svc.CommentTask(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	_, level, err := h.requireTask(r, u, taskID, tasks.LevelView)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteComment(r.Context(), u.ID, id, level >= tasks.LevelManage)
}

// people: names for everyone signed in; emails only for callers who manage
// spaces (TASK-33). Supports ?q= search and ?limit=.
func (h *taskHandler) people(r *http.Request, u *auth.User) (int, any, error) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	orgID, err := h.svc.DefaultOrganization(r.Context())
	if err != nil {
		return 0, nil, err
	}
	out, err := h.svc.ListPeople(r.Context(), r.URL.Query().Get("q"), limit, u.HasInOrg(domain.PermSpaceManage, orgID))
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"users": out}, nil
}
