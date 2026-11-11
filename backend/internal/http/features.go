package http

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Phase-4 feature handlers: tags, checklists, watchers and the Inbox,
// attachments, search, folders, favourites, trash and comment editing.

// ----------------------------------------------------------------------------
// Tags (TASK-4): listing needs view, changing needs manage on the space.
// ----------------------------------------------------------------------------

func (h *taskHandler) listTags(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.SpaceTags(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"tags": out}, nil
}

func (h *taskHandler) createTag(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.TagInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	t, err := h.svc.CreateTag(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, t, nil
}

func (h *taskHandler) updateTag(r *http.Request, u *auth.User) (int, any, error) {
	spaceID, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	tagID, err := parseUUIDParam(r, "tag")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, spaceID, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.TagInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	t, err := h.svc.UpdateTag(r.Context(), spaceID, tagID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, t, nil
}

func (h *taskHandler) deleteTag(r *http.Request, u *auth.User) (int, any, error) {
	spaceID, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	tagID, err := parseUUIDParam(r, "tag")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, spaceID, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteTag(r.Context(), spaceID, tagID)
}

// ----------------------------------------------------------------------------
// Checklists (TASK-6): edit on the task.
// ----------------------------------------------------------------------------

func (h *taskHandler) addChecklistItem(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	item, err := h.svc.AddChecklistItem(r.Context(), u.ID, id, in.Body)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, item, nil
}

func (h *taskHandler) requireChecklistItem(r *http.Request, u *auth.User) (uuid.UUID, error) {
	itemID, err := parseUUIDParam(r, "item")
	if err != nil {
		return uuid.Nil, err
	}
	taskID, err := h.svc.ChecklistItemTask(r.Context(), itemID)
	if err != nil {
		return uuid.Nil, err
	}
	if _, _, err := h.requireTask(r, u, taskID, tasks.LevelEdit); err != nil {
		return uuid.Nil, err
	}
	return itemID, nil
}

func (h *taskHandler) updateChecklistItem(r *http.Request, u *auth.User) (int, any, error) {
	itemID, err := h.requireChecklistItem(r, u)
	if err != nil {
		return 0, nil, err
	}
	var p tasks.ChecklistPatch
	if err := decodeJSON(r, &p); err != nil {
		return 0, nil, err
	}
	item, err := h.svc.UpdateChecklistItem(r.Context(), u.ID, itemID, p)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, item, nil
}

func (h *taskHandler) deleteChecklistItem(r *http.Request, u *auth.User) (int, any, error) {
	itemID, err := h.requireChecklistItem(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteChecklistItem(r.Context(), itemID)
}

// ----------------------------------------------------------------------------
// Watchers and the Inbox (TASK-5)
// ----------------------------------------------------------------------------

func (h *taskHandler) watch(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.Watch(r.Context(), u.ID, id)
}

func (h *taskHandler) unwatch(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	// No access check: unsubscribing from something you lost access to
	// must always work.
	return http.StatusNoContent, nil, h.svc.Unwatch(r.Context(), u.ID, id)
}

func (h *taskHandler) inbox(r *http.Request, u *auth.User) (int, any, error) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	unread := r.URL.Query().Get("unread") == "1"
	out, err := h.svc.Inbox(r.Context(), u.ID, unread, limit)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"notifications": out}, nil
}

func (h *taskHandler) unreadCount(r *http.Request, u *auth.User) (int, any, error) {
	n, err := h.svc.UnreadCount(r.Context(), u.ID)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"count": n}, nil
}

func (h *taskHandler) markRead(r *http.Request, u *auth.User) (int, any, error) {
	var in struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.MarkRead(r.Context(), u.ID, in.IDs)
}

// ----------------------------------------------------------------------------
// Search (TASK-8)
// ----------------------------------------------------------------------------

func (h *taskHandler) search(r *http.Request, u *auth.User) (int, any, error) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	spaces, _, err := h.svc.ListSpaces(r.Context(), false)
	if err != nil {
		return 0, nil, err
	}
	acc, err := h.svc.AccessFor(r.Context(), u.ID, u.Groups)
	if err != nil {
		return 0, nil, err
	}
	var viewIDs, manageIDs []uuid.UUID
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
		switch {
		case level >= tasks.LevelManage:
			manageIDs = append(manageIDs, sp.ID)
		case level >= tasks.LevelView:
			viewIDs = append(viewIDs, sp.ID)
		}
	}
	var listIDs []uuid.UUID
	for id, level := range acc.ListLevels {
		if level >= tasks.LevelView {
			listIDs = append(listIDs, id)
		}
	}
	hits, err := h.svc.Search(r.Context(), q, u.ID, viewIDs, manageIDs, listIDs, limit)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"results": hits}, nil
}

// ----------------------------------------------------------------------------
// Folders (TASK-9): manage on the space.
// ----------------------------------------------------------------------------

func (h *taskHandler) createFolder(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	var in tasks.FolderInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	f, err := h.svc.CreateFolder(r.Context(), id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, f, nil
}

func (h *taskHandler) requireFolder(r *http.Request, u *auth.User) (uuid.UUID, error) {
	folderID, err := parseUUIDParam(r, "folder")
	if err != nil {
		return uuid.Nil, err
	}
	spaceID, err := h.svc.FolderSpace(r.Context(), folderID)
	if err != nil {
		return uuid.Nil, err
	}
	if _, _, err := h.requireSpace(r, u, spaceID, tasks.LevelManage); err != nil {
		return uuid.Nil, err
	}
	return folderID, nil
}

func (h *taskHandler) updateFolder(r *http.Request, u *auth.User) (int, any, error) {
	folderID, err := h.requireFolder(r, u)
	if err != nil {
		return 0, nil, err
	}
	var in tasks.FolderInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	f, err := h.svc.UpdateFolder(r.Context(), folderID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, f, nil
}

func (h *taskHandler) deleteFolder(r *http.Request, u *auth.User) (int, any, error) {
	folderID, err := h.requireFolder(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteFolder(r.Context(), folderID)
}

// ----------------------------------------------------------------------------
// Favourites (TASK-11): personal, so only existence checks apply.
// ----------------------------------------------------------------------------

func (h *taskHandler) listFavorites(r *http.Request, u *auth.User) (int, any, error) {
	out, err := h.svc.Favorites(r.Context(), u.ID)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"favorites": out}, nil
}

func (h *taskHandler) favoriteBody(r *http.Request, u *auth.User) (tasks.Favorite, error) {
	var f tasks.Favorite
	if err := decodeJSON(r, &f); err != nil {
		return f, err
	}
	// Favouriting something you cannot see would leak its existence.
	if f.SpaceID != nil {
		if _, _, err := h.requireSpace(r, u, *f.SpaceID, tasks.LevelView); err != nil {
			return f, err
		}
	}
	if f.ListID != nil {
		if _, _, _, err := h.requireList(r, u, *f.ListID, tasks.LevelView); err != nil {
			return f, err
		}
	}
	return f, nil
}

func (h *taskHandler) addFavorite(r *http.Request, u *auth.User) (int, any, error) {
	f, err := h.favoriteBody(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.AddFavorite(r.Context(), u.ID, f)
}

func (h *taskHandler) removeFavorite(r *http.Request, u *auth.User) (int, any, error) {
	var f tasks.Favorite
	if err := decodeJSON(r, &f); err != nil {
		return 0, nil, err
	}
	// No access check: unpinning must always work.
	return http.StatusNoContent, nil, h.svc.RemoveFavorite(r.Context(), u.ID, f)
}

// ----------------------------------------------------------------------------
// Automations (TASK-22): space managers run the whole space's rules; a
// manager of just one list can manage rules scoped to that list.
// ----------------------------------------------------------------------------

// canManageAutomation: manage on the space, or manage on the rule's list.
func (h *taskHandler) canManageAutomation(r *http.Request, u *auth.User, spaceID uuid.UUID, listID *uuid.UUID) error {
	_, _, spaceErr := h.requireSpace(r, u, spaceID, tasks.LevelManage)
	if spaceErr == nil {
		return nil
	}
	if listID != nil {
		if _, _, _, err := h.requireList(r, u, *listID, tasks.LevelManage); err == nil {
			return nil
		}
	}
	return spaceErr
}

func (h *taskHandler) listAutomations(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	all, err := h.svc.SpaceAutomations(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err == nil {
		return http.StatusOK, map[string]any{"automations": all}, nil
	}
	// Not a space manager: show only rules on lists this user manages.
	visible := []tasks.Automation{}
	for _, a := range all {
		if a.ListID == nil {
			continue
		}
		if _, _, _, err := h.requireList(r, u, *a.ListID, tasks.LevelManage); err == nil {
			visible = append(visible, a)
		}
	}
	if len(visible) == 0 {
		// Same answer a stranger gets, so nothing about the space leaks.
		if _, _, err := h.requireSpace(r, u, id, tasks.LevelView); err != nil {
			return 0, nil, err
		}
	}
	return http.StatusOK, map[string]any{"automations": visible}, nil
}

func (h *taskHandler) createAutomation(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	var in tasks.AutomationInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	if err := h.canManageAutomation(r, u, id, in.ListID.Value); err != nil {
		return 0, nil, err
	}
	a, err := h.svc.CreateAutomation(r.Context(), u.ID, id, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, a, nil
}

func (h *taskHandler) requireAutomation(r *http.Request, u *auth.User) (tasks.Automation, error) {
	id, err := parseUUIDParam(r, "automation")
	if err != nil {
		return tasks.Automation{}, err
	}
	a, err := h.svc.GetAutomation(r.Context(), id)
	if err != nil {
		return tasks.Automation{}, err
	}
	if err := h.canManageAutomation(r, u, a.SpaceID, a.ListID); err != nil {
		return tasks.Automation{}, err
	}
	return a, nil
}

func (h *taskHandler) updateAutomation(r *http.Request, u *auth.User) (int, any, error) {
	a, err := h.requireAutomation(r, u)
	if err != nil {
		return 0, nil, err
	}
	var in tasks.AutomationInput
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	// Rescoping to another list needs rights on that list too.
	if in.ListID.Set {
		if err := h.canManageAutomation(r, u, a.SpaceID, in.ListID.Value); err != nil {
			return 0, nil, err
		}
	}
	out, err := h.svc.UpdateAutomation(r.Context(), a.ID, in)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, out, nil
}

func (h *taskHandler) deleteAutomation(r *http.Request, u *auth.User) (int, any, error) {
	a, err := h.requireAutomation(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.DeleteAutomation(r.Context(), a.ID)
}

func (h *taskHandler) automationRuns(r *http.Request, u *auth.User) (int, any, error) {
	a, err := h.requireAutomation(r, u)
	if err != nil {
		return 0, nil, err
	}
	runs, err := h.svc.AutomationRuns(r.Context(), a.ID, 50)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"runs": runs}, nil
}

// ----------------------------------------------------------------------------
// Archived lists (TASK-48): managers see them so they can restore them.
// ----------------------------------------------------------------------------

func (h *taskHandler) archivedLists(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.ArchivedLists(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"lists": out}, nil
}

// ----------------------------------------------------------------------------
// Trash (TASK-48): manage on the space.
// ----------------------------------------------------------------------------

func (h *taskHandler) trash(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "space")
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireSpace(r, u, id, tasks.LevelManage); err != nil {
		return 0, nil, err
	}
	out, err := h.svc.TrashedTasks(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"tasks": out}, nil
}

// requireTrashedTask: trashed tasks 404 through requireTask, so trash
// operations check manage on the task's space instead.
func (h *taskHandler) requireTrashedTask(r *http.Request, u *auth.User) (uuid.UUID, error) {
	id, err := h.taskID(r)
	if err != nil {
		return uuid.Nil, err
	}
	spaceID, err := h.svc.TaskSpace(r.Context(), id)
	if err != nil {
		return uuid.Nil, err
	}
	if _, _, err := h.requireSpace(r, u, spaceID, tasks.LevelManage); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (h *taskHandler) restoreTask(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.requireTrashedTask(r, u)
	if err != nil {
		return 0, nil, err
	}
	t, err := h.svc.RestoreTask(r.Context(), u.ID, id)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, t, nil
}

func (h *taskHandler) purgeTask(r *http.Request, u *auth.User) (int, any, error) {
	id, err := h.requireTrashedTask(r, u)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.svc.PurgeTask(r.Context(), u.ID, id)
}

// ----------------------------------------------------------------------------
// Comment editing (TASK-47)
// ----------------------------------------------------------------------------

func (h *taskHandler) updateComment(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "comment")
	if err != nil {
		return 0, nil, err
	}
	taskID, _, err := h.svc.CommentTask(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, taskID, tasks.LevelView); err != nil {
		return 0, nil, err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return 0, nil, err
	}
	c, err := h.svc.UpdateComment(r.Context(), u.ID, id, in.Body)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, c, nil
}

// ----------------------------------------------------------------------------
// Attachments (TASK-7). Multipart upload, streamed download; both always go
// through the task's access checks, never via presigned URLs.
// ----------------------------------------------------------------------------

const maxAttachmentSize = 25 << 20 // 25 MB

func (h *taskHandler) uploadAttachment(r *http.Request, u *auth.User) (int, any, error) {
	if h.files == nil {
		return 0, nil, httperror.New(httperror.CodeUnavailable, "attachments are not configured (S3_ENDPOINT is unset)")
	}
	id, err := h.taskID(r)
	if err != nil {
		return 0, nil, err
	}
	if _, _, err := h.requireTask(r, u, id, tasks.LevelEdit); err != nil {
		return 0, nil, err
	}
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return 0, nil, httperror.BadRequest("expected a multipart upload with a 'file' field (max 25 MB)")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return 0, nil, httperror.BadRequest("expected a 'file' field")
	}
	defer file.Close() //nolint:errcheck
	if header.Size > maxAttachmentSize {
		return 0, nil, httperror.Validation("the file is larger than 25 MB", nil)
	}
	contentType := header.Header.Get("Content-Type")
	key := fmt.Sprintf("tasks/%s/%s/%s", id, uuid.New(), sanitizeFilename(header.Filename))
	if err := h.files.PutObject(r.Context(), key, file, header.Size, contentType); err != nil {
		return 0, nil, err
	}
	a, err := h.svc.AddAttachment(r.Context(), u.ID, id, header.Filename, contentType, key, header.Size)
	if err != nil {
		// The metadata row failed; drop the orphaned object, best effort.
		_ = h.files.Remove(r.Context(), key)
		return 0, nil, err
	}
	return http.StatusCreated, a, nil
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = "file"
	}
	return name
}

// downloadAttachment streams the bytes. It bypasses handle() because the
// body is not JSON.
func (h *taskHandler) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	u, err := caller(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	id, err := parseUUIDParam(r, "attachment")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	a, err := h.svc.GetAttachment(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if _, _, err := h.requireTask(r, u, a.TaskID, tasks.LevelView); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.files == nil {
		httperror.Render(w, r, httperror.New(httperror.CodeUnavailable, "attachments are not configured (S3_ENDPOINT is unset)"))
		return
	}
	obj, err := h.files.GetObject(r.Context(), a.S3Key)
	if err != nil {
		httperror.Render(w, r, httperror.New(httperror.CodeUnavailable, "the stored file could not be read"))
		return
	}
	defer obj.Close() //nolint:errcheck
	disposition := "attachment"
	if strings.HasPrefix(a.ContentType, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(a.SizeBytes, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, a.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, obj)
}

func (h *taskHandler) deleteAttachment(r *http.Request, u *auth.User) (int, any, error) {
	id, err := parseUUIDParam(r, "attachment")
	if err != nil {
		return 0, nil, err
	}
	a, err := h.svc.GetAttachment(r.Context(), id)
	if err != nil {
		return 0, nil, err
	}
	_, level, err := h.requireTask(r, u, a.TaskID, tasks.LevelEdit)
	if err != nil {
		return 0, nil, err
	}
	isUploader := a.UploadedBy != nil && *a.UploadedBy == u.ID
	if !isUploader && level < tasks.LevelManage {
		return 0, nil, httperror.New(httperror.CodeForbidden, "only the uploader or a space manager can remove an attachment")
	}
	if err := h.svc.DeleteAttachment(r.Context(), u.ID, id); err != nil {
		return 0, nil, err
	}
	if h.files != nil {
		_ = h.files.Remove(r.Context(), a.S3Key) // orphaned object is harmless
	}
	return http.StatusNoContent, nil, nil
}
