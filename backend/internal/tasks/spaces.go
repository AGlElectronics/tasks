// Package tasks is the task-tracking service layer: spaces with their
// status sets, lists, tasks with subtasks and assignees, comments and the
// activity log. Handlers own authorization; this package owns state and
// invariants and returns typed httperror values for business failures.
package tasks

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
)

// Service is safe for concurrent use.
type Service struct {
	pool *pgxpool.Pool
	// envSMTP is Tasks' own SMTP (SMTP_*), used when Den sends none.
	envSMTP mail.Config
	// publicURL is TASKS_PUBLIC_URL, for links in emails.
	publicURL string
	// groups is the shared group matching rule for group grants.
	groups groupmatch.Config
}

// SetGroupMatch sets the group matching rule (OIDC_GROUP_MATCH,
// OIDC_GROUP_PREFIX) used for group grants.
func (s *Service) SetGroupMatch(c groupmatch.Config) { s.groups = c }

// SetPublicURL sets the web app address used for links in emails.
func (s *Service) SetPublicURL(u string) { s.publicURL = strings.TrimRight(u, "/") }

// NewService constructs the task service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

var (
	keyPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,5}$`)
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Status is one column of a space's workflow.
type Status struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Color    string    `json:"color"`
	Category string    `json:"category"`
	Position int       `json:"position"`
}

// List is a list of tasks inside a space.
type List struct {
	ID          uuid.UUID  `json:"id"`
	SpaceID     uuid.UUID  `json:"space_id"`
	FolderID    *uuid.UUID `json:"folder_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Position    int        `json:"position"`
	Private     bool       `json:"private"`
	Archived    bool       `json:"archived"`
	OpenTasks   int        `json:"open_tasks"`
	DoneTasks   int        `json:"done_tasks"`
}

// Space is a top-level container, usually a department or a big project.
type Space struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Key         string     `json:"key"`
	Color       string     `json:"color"`
	Description string     `json:"description"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	// EveryoneLevel is the level every signed-in user gets; nil = closed.
	EveryoneLevel *string `json:"everyone_level"`
	// Archived spaces are hidden from the sidebar and search.
	Archived bool `json:"archived"`
	// DepartmentID is the HR department this space belongs to (set by the
	// department hook); nil for spaces made by hand.
	DepartmentID *uuid.UUID `json:"department_id"`
	Lists        []List     `json:"lists"`
	Folders      []Folder   `json:"folders"`
	Statuses     []Status   `json:"statuses"`
	OrgID        uuid.UUID  `json:"-"`
}

var defaultStatuses = []Status{
	{Name: "To do", Color: "#9ca3af", Category: domain.CategoryNotStarted},
	{Name: "In progress", Color: "#3b82f6", Category: domain.CategoryActive},
	{Name: "Review", Color: "#a855f7", Category: domain.CategoryActive},
	{Name: "Done", Color: "#22c55e", Category: domain.CategoryDone},
}

// MaxSpaces bounds the sidebar query. An organization has tens of spaces;
// the cap only keeps a runaway import from making every page load slow.
const MaxSpaces = 1000

// ListSpaces returns up to MaxSpaces spaces with their lists and statuses
// (sidebar), archived ones only when asked. truncated reports that more
// spaces exist.
func (s *Service) ListSpaces(ctx context.Context, includeArchived bool) (spaces []Space, truncated bool, err error) {
	spaces, err = s.listSpaces(ctx, includeArchived)
	if len(spaces) > MaxSpaces {
		spaces, truncated = spaces[:MaxSpaces], true
	}
	return spaces, truncated, err
}

func (s *Service) listSpaces(ctx context.Context, includeArchived bool) ([]Space, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, organization_id, name, key, color, description, created_by, created_at, everyone_level,
		       archived_at IS NOT NULL, department_id
		  FROM spaces WHERE $2 OR archived_at IS NULL ORDER BY position, name LIMIT $1`, MaxSpaces+1, includeArchived)
	if err != nil {
		return nil, err
	}
	spaces := []Space{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.OrgID, &sp.Name, &sp.Key, &sp.Color, &sp.Description, &sp.CreatedBy, &sp.CreatedAt, &sp.EveryoneLevel, &sp.Archived, &sp.DepartmentID); err != nil {
			rows.Close()
			return nil, err
		}
		sp.Lists, sp.Folders, sp.Statuses = []List{}, []Folder{}, []Status{}
		index[sp.ID] = len(spaces)
		spaces = append(spaces, sp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lists, err := s.lists(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, l := range lists {
		if i, ok := index[l.SpaceID]; ok {
			spaces[i].Lists = append(spaces[i].Lists, l)
		}
	}
	frows, err := s.pool.Query(ctx, `SELECT id, space_id, name, position FROM folders ORDER BY position, created_at`)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var f Folder
		if err := frows.Scan(&f.ID, &f.SpaceID, &f.Name, &f.Position); err != nil {
			frows.Close()
			return nil, err
		}
		if i, ok := index[f.SpaceID]; ok {
			spaces[i].Folders = append(spaces[i].Folders, f)
		}
	}
	frows.Close()
	if err := frows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT space_id, id, name, color, category, position FROM statuses ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var spaceID uuid.UUID
		var st Status
		if err := rows.Scan(&spaceID, &st.ID, &st.Name, &st.Color, &st.Category, &st.Position); err != nil {
			return nil, err
		}
		if i, ok := index[spaceID]; ok {
			spaces[i].Statuses = append(spaces[i].Statuses, st)
		}
	}
	return spaces, rows.Err()
}

func (s *Service) lists(ctx context.Context, spaceID *uuid.UUID) ([]List, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.space_id, l.folder_id, l.name, l.description, l.position, l.private, l.archived_at IS NOT NULL,
		       count(t.id) FILTER (WHERE st.category <> 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL),
		       count(t.id) FILTER (WHERE st.category = 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL)
		  FROM lists l
		  LEFT JOIN tasks t ON t.list_id = l.id
		  LEFT JOIN statuses st ON st.id = t.status_id
		 WHERE l.archived_at IS NULL AND ($1::uuid IS NULL OR l.space_id = $1)
		 GROUP BY l.id
		 ORDER BY l.position, l.created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.SpaceID, &l.FolderID, &l.Name, &l.Description, &l.Position, &l.Private, &l.Archived, &l.OpenTasks, &l.DoneTasks); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ArchivedLists returns a space's archived lists (shown to managers so they
// can restore them, TASK-48).
func (s *Service) ArchivedLists(ctx context.Context, spaceID uuid.UUID) ([]List, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.space_id, l.folder_id, l.name, l.description, l.position, l.private, true,
		       count(t.id) FILTER (WHERE st.category <> 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL),
		       count(t.id) FILTER (WHERE st.category = 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL)
		  FROM lists l
		  LEFT JOIN tasks t ON t.list_id = l.id
		  LEFT JOIN statuses st ON st.id = t.status_id
		 WHERE l.archived_at IS NOT NULL AND l.space_id = $1
		 GROUP BY l.id
		 ORDER BY l.position, l.created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.SpaceID, &l.FolderID, &l.Name, &l.Description, &l.Position, &l.Private, &l.Archived, &l.OpenTasks, &l.DoneTasks); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Service) statuses(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, spaceID uuid.UUID) ([]Status, error) {
	rows, err := q.Query(ctx, `SELECT id, name, color, category, position FROM statuses WHERE space_id = $1 ORDER BY position`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		var st Status
		if err := rows.Scan(&st.ID, &st.Name, &st.Color, &st.Category, &st.Position); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// GetSpace loads one space with its lists and statuses, or a typed 404.
func (s *Service) GetSpace(ctx context.Context, id uuid.UUID) (Space, error) {
	var sp Space
	err := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, name, key, color, description, created_by, created_at, everyone_level,
		       archived_at IS NOT NULL, department_id
		  FROM spaces WHERE id = $1`, id,
	).Scan(&sp.ID, &sp.OrgID, &sp.Name, &sp.Key, &sp.Color, &sp.Description, &sp.CreatedBy, &sp.CreatedAt, &sp.EveryoneLevel, &sp.Archived, &sp.DepartmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Space{}, httperror.NotFound("space")
	}
	if err != nil {
		return Space{}, err
	}
	if sp.Lists, err = s.lists(ctx, &sp.ID); err != nil {
		return Space{}, err
	}
	if sp.Folders, err = s.SpaceFolders(ctx, sp.ID); err != nil {
		return Space{}, err
	}
	sp.Statuses, err = s.statuses(ctx, s.pool, sp.ID)
	return sp, err
}

// SpaceInput creates or updates a space. EveryoneLevel sent as null closes
// the space to non-granted users; absent leaves it alone.
type SpaceInput struct {
	Name          *string  `json:"name"`
	Key           *string  `json:"key"`
	Color         *string  `json:"color"`
	Description   *string  `json:"description"`
	EveryoneLevel Nullable `json:"everyone_level"`
	// Archived hides or restores the space. For department spaces HR's
	// next save wins (the hook sends the department's archived state).
	Archived *bool `json:"archived"`
}

func (in *SpaceInput) validate(create bool) error {
	issues := map[string]string{}
	if create || in.Name != nil {
		if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
			issues["name"] = "required"
		}
	}
	if create || in.Key != nil {
		if in.Key == nil || !keyPattern.MatchString(strings.ToUpper(strings.TrimSpace(*in.Key))) {
			issues["key"] = "2–6 letters or digits, starting with a letter"
		}
	}
	if in.Color != nil && !colorPattern.MatchString(*in.Color) {
		issues["color"] = "must look like #1a2b3c"
	}
	if in.EveryoneLevel.Set && in.EveryoneLevel.Value != nil && ParseLevel(*in.EveryoneLevel.Value) == LevelNone {
		issues["everyone_level"] = "must be view, edit, manage or null"
	}
	if len(issues) > 0 {
		return httperror.Validation("invalid space", issues)
	}
	return nil
}

// CreateSpace makes a space with the default statuses and a first list.
func (s *Service) CreateSpace(ctx context.Context, orgID, userID uuid.UUID, in SpaceInput) (Space, error) {
	if err := in.validate(true); err != nil {
		return Space{}, err
	}
	color := "#6366f1"
	if in.Color != nil {
		color = *in.Color
	}
	desc := ""
	if in.Description != nil {
		desc = *in.Description
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Space{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO spaces (organization_id, name, key, color, description, created_by, position)
		VALUES ($1, $2, $3, $4, $5, $6, (SELECT COALESCE(max(position), 0) + 1 FROM spaces))
		RETURNING id`,
		orgID, strings.TrimSpace(*in.Name), strings.ToUpper(strings.TrimSpace(*in.Key)), color, desc, userID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return Space{}, httperror.Conflict("another space already uses this key")
		}
		return Space{}, err
	}
	for i, st := range defaultStatuses {
		if _, err := tx.Exec(ctx, `INSERT INTO statuses (space_id, name, color, category, position) VALUES ($1, $2, $3, $4, $5)`,
			id, st.Name, st.Color, st.Category, i); err != nil {
			return Space{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lists (space_id, name) VALUES ($1, 'General')`, id); err != nil {
		return Space{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Space{}, err
	}
	return s.GetSpace(ctx, id)
}

// UpdateSpace applies the non-nil fields. Changing the key renames every
// task ID in the space; the old KEY-N references are aliased in task_ids so
// they keep resolving (TASK-45).
func (s *Service) UpdateSpace(ctx context.Context, id uuid.UUID, in SpaceInput) (Space, error) {
	if err := in.validate(false); err != nil {
		return Space{}, err
	}
	var key *string
	if in.Key != nil {
		k := strings.ToUpper(strings.TrimSpace(*in.Key))
		key = &k
	}
	var name *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		name = &n
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Space{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if key != nil {
		// Alias every task's current KEY-N before the rename (no-op when
		// the key is unchanged thanks to the WHERE).
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_ids (key, task_id)
			SELECT sp.key || '-' || t.number, t.id
			  FROM tasks t JOIN spaces sp ON sp.id = t.space_id
			 WHERE sp.id = $1 AND sp.key <> $2
			ON CONFLICT (key) DO UPDATE SET task_id = EXCLUDED.task_id`, id, *key); err != nil {
			return Space{}, err
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE spaces SET name = COALESCE($2, name), key = COALESCE($3, key), color = COALESCE($4, color),
		       description = COALESCE($5, description),
		       everyone_level = CASE WHEN $6 THEN $7 ELSE everyone_level END,
		       archived_at = CASE WHEN $8::bool IS NULL THEN archived_at
		                          WHEN $8 THEN COALESCE(archived_at, now())
		                          ELSE NULL END,
		       updated_at = now()
		 WHERE id = $1`, id, name, key, in.Color, in.Description, in.EveryoneLevel.Set, in.EveryoneLevel.Value, in.Archived)
	if err != nil {
		if isUniqueViolation(err) {
			return Space{}, httperror.Conflict("another space already uses this key")
		}
		return Space{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Space{}, err
	}
	return s.GetSpace(ctx, id)
}

// DeleteSpace removes a space with every list and task in it, leaving an
// audit_log row (TASK-29).
func (s *Service) DeleteSpace(ctx context.Context, actor, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var name, key string
	var taskCount int
	err = tx.QueryRow(ctx, `
		SELECT sp.name, sp.key, (SELECT count(*) FROM tasks t WHERE t.space_id = sp.id)
		  FROM spaces sp WHERE sp.id = $1`, id).Scan(&name, &key, &taskCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("space")
	}
	if err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, &actor, "space", id, "deleted", map[string]any{"name": name, "key": key, "tasks": taskCount}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM spaces WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// StatusInput is one status in a replacement set. ID nil = new status.
type StatusInput struct {
	ID       *uuid.UUID `json:"id"`
	Name     string     `json:"name"`
	Color    string     `json:"color"`
	Category string     `json:"category"`
}

// ReplaceStatuses sets a space's statuses to exactly `in`, in order. Tasks
// on a removed status move to the first remaining status of the same
// category (or the first status); each move is logged to the task's
// activity (TASK-44) under the acting user.
func (s *Service) ReplaceStatuses(ctx context.Context, actor, spaceID uuid.UUID, in []StatusInput) ([]Status, error) {
	if len(in) == 0 {
		return nil, httperror.Validation("a space needs at least one status", nil)
	}
	hasOpen, hasDone := false, false
	for i, st := range in {
		in[i].Name = strings.TrimSpace(st.Name)
		if in[i].Name == "" {
			return nil, httperror.Validation("every status needs a name", nil)
		}
		if !colorPattern.MatchString(st.Color) {
			return nil, httperror.Validation("invalid colour for "+in[i].Name, nil)
		}
		switch st.Category {
		case domain.CategoryNotStarted, domain.CategoryActive:
			hasOpen = true
		case domain.CategoryDone:
			hasDone = true
		default:
			return nil, httperror.Validation("category must be not_started, active or done", nil)
		}
	}
	if !hasOpen || !hasDone {
		return nil, httperror.Validation("keep at least one open and one done status", nil)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	existing, err := s.statuses(ctx, tx, spaceID)
	if err != nil {
		return nil, err
	}
	keep := map[uuid.UUID]bool{}
	for i, st := range in {
		if st.ID != nil {
			tag, err := tx.Exec(ctx, `UPDATE statuses SET name = $3, color = $4, category = $5, position = $6 WHERE id = $1 AND space_id = $2`,
				*st.ID, spaceID, st.Name, st.Color, st.Category, i)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, httperror.Validation("unknown status id "+st.ID.String(), nil)
			}
			keep[*st.ID] = true
			continue
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO statuses (space_id, name, color, category, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			spaceID, st.Name, st.Color, st.Category, i).Scan(&id); err != nil {
			return nil, err
		}
		keep[id] = true
	}
	for _, old := range existing {
		if keep[old.ID] {
			continue
		}
		var targetID uuid.UUID
		var targetName string
		err := tx.QueryRow(ctx, `SELECT id, name FROM statuses WHERE space_id = $2 AND category = $3 AND id <> $1 ORDER BY position LIMIT 1`,
			old.ID, spaceID, old.Category).Scan(&targetID, &targetName)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id, name FROM statuses WHERE space_id = $2 AND id <> $1 ORDER BY position LIMIT 1`,
				old.ID, spaceID).Scan(&targetID, &targetName)
		}
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `
			UPDATE tasks SET status_id = $2, updated_at = now(), version = version + 1
			 WHERE status_id = $1 RETURNING id`, old.ID, targetID)
		if err != nil {
			return nil, err
		}
		var moved []uuid.UUID
		for rows.Next() {
			var tid uuid.UUID
			if err := rows.Scan(&tid); err != nil {
				rows.Close()
				return nil, err
			}
			moved = append(moved, tid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, tid := range moved {
			if err := addEvent(ctx, tx, tid, actor, "status", map[string]any{"from": old.Name, "to": targetName, "cause": "status removed"}); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM statuses WHERE id = $1`, old.ID); err != nil {
			return nil, err
		}
	}
	// Category changes can complete or reopen tasks; each one that flips is
	// logged like any other completion.
	rows, err := tx.Query(ctx, `
		UPDATE tasks t SET completed_at = CASE WHEN st.category = 'done' THEN now() ELSE NULL END
		  FROM statuses st
		 WHERE st.id = t.status_id AND t.space_id = $1
		   AND (st.category = 'done') <> (t.completed_at IS NOT NULL)
		RETURNING t.id, st.category = 'done'`, spaceID)
	if err != nil {
		return nil, err
	}
	type flip struct {
		id   uuid.UUID
		done bool
	}
	var flips []flip
	for rows.Next() {
		var f flip
		if err := rows.Scan(&f.id, &f.done); err != nil {
			rows.Close()
			return nil, err
		}
		flips = append(flips, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, f := range flips {
		kind := "reopened"
		if f.done {
			kind = "completed"
		}
		if err := addEvent(ctx, tx, f.id, actor, kind, map[string]any{"cause": "status category changed"}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.statuses(ctx, s.pool, spaceID)
}

// ListInput creates or updates a list. FolderID sent as null moves the list
// back to the space root; Archived true/false archives or restores it.
type ListInput struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	Position    *int         `json:"position"`
	Private     *bool        `json:"private"`
	FolderID    NullableUUID `json:"folder_id"`
	Archived    *bool        `json:"archived"`
}

// CreateList adds a list at the end of a space.
func (s *Service) CreateList(ctx context.Context, spaceID uuid.UUID, in ListInput) (List, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return List{}, httperror.Validation("invalid list", map[string]string{"name": "required"})
	}
	desc := ""
	if in.Description != nil {
		desc = *in.Description
	}
	private := in.Private != nil && *in.Private
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO lists (space_id, name, description, private, position)
		VALUES ($1, $2, $3, $4, (SELECT COALESCE(max(position), 0) + 1 FROM lists WHERE space_id = $1))
		RETURNING id`, spaceID, strings.TrimSpace(*in.Name), desc, private).Scan(&id)
	if err != nil {
		return List{}, err
	}
	return s.GetList(ctx, id)
}

// GetList loads one list or a typed 404. Archived lists load too (their
// Archived flag is set), so they can be inspected and restored.
func (s *Service) GetList(ctx context.Context, id uuid.UUID) (List, error) {
	var l List
	err := s.pool.QueryRow(ctx, `
		SELECT l.id, l.space_id, l.folder_id, l.name, l.description, l.position, l.private, l.archived_at IS NOT NULL,
		       count(t.id) FILTER (WHERE st.category <> 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL),
		       count(t.id) FILTER (WHERE st.category = 'done' AND t.parent_id IS NULL AND t.deleted_at IS NULL)
		  FROM lists l LEFT JOIN tasks t ON t.list_id = l.id LEFT JOIN statuses st ON st.id = t.status_id
		 WHERE l.id = $1 GROUP BY l.id`, id,
	).Scan(&l.ID, &l.SpaceID, &l.FolderID, &l.Name, &l.Description, &l.Position, &l.Private, &l.Archived, &l.OpenTasks, &l.DoneTasks)
	if errors.Is(err, pgx.ErrNoRows) {
		return List{}, httperror.NotFound("list")
	}
	return l, err
}

// UpdateList applies the non-nil fields.
func (s *Service) UpdateList(ctx context.Context, id uuid.UUID, in ListInput) (List, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return List{}, httperror.Validation("invalid list", map[string]string{"name": "required"})
	}
	var name *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		name = &n
	}
	if in.FolderID.Set && in.FolderID.Value != nil {
		// A folder only groups lists of its own space.
		cur, err := s.GetList(ctx, id)
		if err != nil {
			return List{}, err
		}
		folderSpace, err := s.FolderSpace(ctx, *in.FolderID.Value)
		if err != nil {
			return List{}, err
		}
		if folderSpace != cur.SpaceID {
			return List{}, httperror.Validation("the folder is in another space", nil)
		}
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE lists SET name = COALESCE($2, name), description = COALESCE($3, description),
		       position = COALESCE($4, position), private = COALESCE($5, private),
		       folder_id = CASE WHEN $6 THEN $7 ELSE folder_id END,
		       archived_at = CASE WHEN $8::bool IS NULL THEN archived_at
		                          WHEN $8 THEN COALESCE(archived_at, now())
		                          ELSE NULL END
		 WHERE id = $1`, id, name, in.Description, in.Position, in.Private,
		in.FolderID.Set, in.FolderID.Value, in.Archived); err != nil {
		return List{}, err
	}
	return s.GetList(ctx, id)
}

// DeleteList removes a list and its tasks, leaving an audit_log row
// (TASK-29). The "keep at least one list" check and the delete happen in
// one transaction with the space row locked (TASK-42), so two concurrent
// deletes cannot empty a space.
func (s *Service) DeleteList(ctx context.Context, actor, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var spaceID uuid.UUID
	var name string
	var taskCount int
	err = tx.QueryRow(ctx, `
		SELECT l.space_id, l.name, (SELECT count(*) FROM tasks t WHERE t.list_id = l.id)
		  FROM lists l WHERE l.id = $1`, id).Scan(&spaceID, &name, &taskCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("list")
	}
	if err != nil {
		return err
	}
	// Serialize list deletes per space: the second delete waits here and
	// then sees the first one's result in its count.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM spaces WHERE id = $1 FOR UPDATE`, spaceID); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM lists WHERE space_id = $1 AND archived_at IS NULL`, spaceID).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return httperror.Conflict("a space needs at least one list")
	}
	if err := writeAudit(ctx, tx, &actor, "list", id, "deleted", map[string]any{"name": name, "space_id": spaceID, "tasks": taskCount}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM lists WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ----------------------------------------------------------------------------
// People
// ----------------------------------------------------------------------------

// Person is a users row projected for pickers.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email,omitempty"`
}

// ListPeople returns people who have signed in to Tasks, filtered by an
// optional name search and capped. Emails are only included for callers
// who manage spaces (TASK-33): the picker needs names, not addresses.
func (s *Service) ListPeople(ctx context.Context, q string, limit int, withEmail bool) ([]Person, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q = strings.TrimSpace(q)
	rows, err := s.pool.Query(ctx, `
		SELECT id, display_name, CASE WHEN $3 THEN COALESCE(email, '') ELSE '' END
		  FROM users
		 WHERE $1 = '' OR display_name ILIKE '%' || $1 || '%'
		 ORDER BY display_name
		 LIMIT $2`, q, limit, withEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Email); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DefaultOrganization returns the first organization (single-org slice).
func (s *Service) DefaultOrganization(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM organizations ORDER BY slug LIMIT 1`).Scan(&id)
	return id, err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}
