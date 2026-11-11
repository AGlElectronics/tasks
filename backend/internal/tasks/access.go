package tasks

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Level is an access level on a space or a list. Levels are ordered:
// manage implies edit implies view.
type Level int

// Access levels, weakest first.
const (
	LevelNone Level = iota
	LevelView
	LevelEdit
	LevelManage
)

// ParseLevel maps the stored text to a Level; unknown text means none.
func ParseLevel(s string) Level {
	switch s {
	case "view":
		return LevelView
	case "edit":
		return LevelEdit
	case "manage":
		return LevelManage
	}
	return LevelNone
}

// String returns the stored text for a level ("" for none).
func (l Level) String() string {
	switch l {
	case LevelView:
		return "view"
	case LevelEdit:
		return "edit"
	case LevelManage:
		return "manage"
	}
	return ""
}

// Max returns the higher of two levels.
func (l Level) Max(o Level) Level {
	if o > l {
		return o
	}
	return l
}

// Grant is one sharing entry on a space or a list.
type Grant struct {
	ID          uuid.UUID  `json:"id"`
	SpaceID     *uuid.UUID `json:"space_id,omitempty"`
	ListID      *uuid.UUID `json:"list_id,omitempty"`
	SubjectType string     `json:"subject_type"` // user | group
	// Subject is the user id, or the group name as the admin typed it.
	Subject string `json:"subject"`
	// SubjectKey is what grants match on: the user id, or the normalised
	// group name (groupmatch.NewGrant(...).Stored; see groups.go).
	SubjectKey string `json:"subject_key"`
	// DisplayName resolves user subjects for the UI; groups show verbatim.
	DisplayName string `json:"display_name"`
	Level       string `json:"level"`
}

// GrantScope addresses either a space or a list.
type GrantScope struct {
	SpaceID *uuid.UUID
	ListID  *uuid.UUID
}

// ListGrants returns the sharing entries for a space or list.
func (s *Service) ListGrants(ctx context.Context, scope GrantScope) ([]Grant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.space_id, g.list_id, g.subject_type, g.subject, g.subject_key, g.level,
		       CASE WHEN g.subject_type = 'user'
		            THEN COALESCE((SELECT u.display_name FROM users u WHERE u.id::text = g.subject), 'Former user')
		            ELSE g.subject END
		  FROM access_grants g
		 WHERE ($1::uuid IS NULL OR g.space_id = $1) AND ($2::uuid IS NULL OR g.list_id = $2)
		   AND (($1::uuid IS NOT NULL) OR ($2::uuid IS NOT NULL))
		 ORDER BY g.subject_type, 8`, scope.SpaceID, scope.ListID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.SpaceID, &g.ListID, &g.SubjectType, &g.Subject, &g.SubjectKey, &g.Level, &g.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantInput creates or replaces a grant on the scope.
type GrantInput struct {
	SubjectType string `json:"subject_type"`
	Subject     string `json:"subject"`
	Level       string `json:"level"`
}

// PutGrant upserts one grant on a space or list.
func (s *Service) PutGrant(ctx context.Context, actor uuid.UUID, scope GrantScope, in GrantInput) (Grant, error) {
	in.Subject = strings.TrimSpace(in.Subject)
	issues := map[string]string{}
	if in.SubjectType != "user" && in.SubjectType != "group" {
		issues["subject_type"] = "must be user or group"
	}
	if in.Subject == "" {
		issues["subject"] = "required"
	}
	if ParseLevel(in.Level) == LevelNone {
		issues["level"] = "must be view, edit or manage"
	}
	if in.SubjectType == "user" {
		uid, err := uuid.Parse(in.Subject)
		if err != nil {
			issues["subject"] = "must be a user id"
		} else {
			var ok bool
			if err := s.pool.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, uid).Scan(&ok); errors.Is(err, pgx.ErrNoRows) {
				issues["subject"] = "unknown user"
			} else if err != nil {
				return Grant{}, err
			}
		}
	}
	if in.SubjectType == "group" && subjectKey("group", in.Subject) == "" {
		issues["subject"] = "required"
	}
	if len(issues) > 0 {
		return Grant{}, httperror.Validation("invalid grant", issues)
	}
	key := subjectKey(in.SubjectType, in.Subject)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Re-granting the same group spelled differently updates the level and
	// the name shown in the UI.
	var id uuid.UUID
	if scope.SpaceID != nil {
		err = tx.QueryRow(ctx, `
			INSERT INTO access_grants (space_id, subject_type, subject, subject_key, level, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (space_id, subject_type, subject_key) WHERE space_id IS NOT NULL
			DO UPDATE SET level = EXCLUDED.level, subject = EXCLUDED.subject
			RETURNING id`, *scope.SpaceID, in.SubjectType, in.Subject, key, in.Level, actor).Scan(&id)
	} else {
		err = tx.QueryRow(ctx, `
			INSERT INTO access_grants (list_id, subject_type, subject, subject_key, level, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (list_id, subject_type, subject_key) WHERE list_id IS NOT NULL
			DO UPDATE SET level = EXCLUDED.level, subject = EXCLUDED.subject
			RETURNING id`, *scope.ListID, in.SubjectType, in.Subject, key, in.Level, actor).Scan(&id)
	}
	if err != nil {
		return Grant{}, err
	}
	if err := writeAudit(ctx, tx, &actor, "grant", id, "granted", map[string]any{
		"space_id": scope.SpaceID, "list_id": scope.ListID,
		"subject_type": in.SubjectType, "subject": in.Subject, "level": in.Level,
	}); err != nil {
		return Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Grant{}, err
	}
	var g Grant
	err = s.pool.QueryRow(ctx, `
		SELECT g.id, g.space_id, g.list_id, g.subject_type, g.subject, g.subject_key, g.level,
		       CASE WHEN g.subject_type = 'user'
		            THEN COALESCE((SELECT u.display_name FROM users u WHERE u.id::text = g.subject), 'Former user')
		            ELSE g.subject END
		  FROM access_grants g WHERE g.id = $1`, id,
	).Scan(&g.ID, &g.SpaceID, &g.ListID, &g.SubjectType, &g.Subject, &g.SubjectKey, &g.Level, &g.DisplayName)
	return g, err
}

// DeleteGrant removes one grant by id, scoped so a handler can ensure it
// belongs to the space or list it authorized.
func (s *Service) DeleteGrant(ctx context.Context, actor, id uuid.UUID, scope GrantScope) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var subjectType, subject, level string
	err = tx.QueryRow(ctx, `
		DELETE FROM access_grants
		 WHERE id = $1 AND ($2::uuid IS NULL OR space_id = $2) AND ($3::uuid IS NULL OR list_id = $3)
		RETURNING subject_type, subject, level`,
		id, scope.SpaceID, scope.ListID).Scan(&subjectType, &subject, &level)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("grant")
	}
	if err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, &actor, "grant", id, "revoked", map[string]any{
		"space_id": scope.SpaceID, "list_id": scope.ListID,
		"subject_type": subjectType, "subject": subject, "level": level,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// grantedLevel computes the strongest grant the user or their groups hold
// on a space or on a list (one query).
func (s *Service) grantedLevel(ctx context.Context, scope GrantScope, userID uuid.UUID, groups []string) (Level, error) {
	var lvl int
	keys, prefix, mode := groupMatchArgs(s.groups, groups)
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(max(CASE level WHEN 'view' THEN 1 WHEN 'edit' THEN 2 WHEN 'manage' THEN 3 END), 0)
		  FROM access_grants
		 WHERE ((subject_type = 'user' AND subject_key = $3::text)
		        OR (subject_type = 'group' AND tasks_group_key(subject_key, $5, $6) = ANY($4)))
		   AND (($1::uuid IS NOT NULL AND space_id = $1) OR ($2::uuid IS NOT NULL AND list_id = $2))`,
		scope.SpaceID, scope.ListID, userID.String(), keys, prefix, mode).Scan(&lvl)
	return Level(lvl), err
}

// SpaceGrantLevel is the strongest space-wide level from everyone_level
// plus the user's and their groups' space grants. Org-permission overrides
// (space.manage, creator) are the HTTP layer's business.
func (s *Service) SpaceGrantLevel(ctx context.Context, sp Space, userID uuid.UUID, groups []string) (Level, error) {
	level := LevelNone
	if sp.EveryoneLevel != nil {
		level = ParseLevel(*sp.EveryoneLevel)
	}
	granted, err := s.grantedLevel(ctx, GrantScope{SpaceID: &sp.ID}, userID, groups)
	if err != nil {
		return LevelNone, err
	}
	return level.Max(granted), nil
}

// ListGrantLevel is the strongest level granted directly on one list.
func (s *Service) ListGrantLevel(ctx context.Context, listID, userID uuid.UUID, groups []string) (Level, error) {
	return s.grantedLevel(ctx, GrantScope{ListID: &listID}, userID, groups)
}

// UserAccess is everything the sidebar needs to decide what the caller
// sees: per-space and per-list granted levels, in two maps.
type UserAccess struct {
	SpaceLevels map[uuid.UUID]Level
	ListLevels  map[uuid.UUID]Level
}

// AccessFor loads every grant that applies to the user in one query.
func (s *Service) AccessFor(ctx context.Context, userID uuid.UUID, groups []string) (UserAccess, error) {
	acc := UserAccess{SpaceLevels: map[uuid.UUID]Level{}, ListLevels: map[uuid.UUID]Level{}}
	keys, prefix, mode := groupMatchArgs(s.groups, groups)
	rows, err := s.pool.Query(ctx, `
		SELECT space_id, list_id, max(CASE level WHEN 'view' THEN 1 WHEN 'edit' THEN 2 WHEN 'manage' THEN 3 END)
		  FROM access_grants
		 WHERE (subject_type = 'user' AND subject_key = $1::text)
		    OR (subject_type = 'group' AND tasks_group_key(subject_key, $3, $4) = ANY($2))
		 GROUP BY space_id, list_id`, userID.String(), keys, prefix, mode)
	if err != nil {
		return acc, err
	}
	defer rows.Close()
	for rows.Next() {
		var spaceID, listID *uuid.UUID
		var lvl int
		if err := rows.Scan(&spaceID, &listID, &lvl); err != nil {
			return acc, err
		}
		if spaceID != nil {
			acc.SpaceLevels[*spaceID] = Level(lvl)
		}
		if listID != nil {
			acc.ListLevels[*listID] = Level(lvl)
		}
	}
	return acc, rows.Err()
}

// IsAssigned reports whether the user is assigned to the task. Assignment
// opens that one task for someone with no space or list access.
func (s *Service) IsAssigned(ctx context.Context, taskID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT true FROM task_assignees WHERE task_id = $1 AND user_id = $2`, taskID, userID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}
