package tasks

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// DepartmentSpaceInput is what HR sends to POST /api/internal/departments
// when a department is created, saved or archived (TASK-25). HR may add
// fields; unknown ones are ignored.
type DepartmentSpaceInput struct {
	ID    uuid.UUID `json:"id"`    // HR department id
	Slug  string    `json:"slug"`  // e.g. front-of-house
	Name  string    `json:"name"`  // e.g. Front of House
	Group string    `json:"group"` // IdP group, e.g. dept-front-of-house
	// Archived is the department's archived state; absent means active.
	Archived bool `json:"archived"`
}

// EnsureDepartmentSpace guarantees exactly one space per HR department. The
// first call for a department creates a closed space (grant: the
// department's IdP group at edit) with the default department lists; later
// calls keep the space's name and archived state in sync with HR. Lists,
// statuses, extra grants and everything else inside the space stay in the
// department's hands and are never touched again; the group is only used
// when the space is created.
//
// Returns the space and whether this call created it.
func (s *Service) EnsureDepartmentSpace(ctx context.Context, orgID uuid.UUID, in DepartmentSpaceInput) (Space, bool, error) {
	sp, created, err := s.ensureDepartmentSpace(ctx, orgID, in)
	if isUniqueViolation(err) {
		// Two first calls raced; the other one created the space, so this
		// one is an update.
		sp, created, err = s.ensureDepartmentSpace(ctx, orgID, in)
	}
	return sp, created, err
}

func (s *Service) ensureDepartmentSpace(ctx context.Context, orgID uuid.UUID, in DepartmentSpaceInput) (Space, bool, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	in.Group = strings.TrimSpace(in.Group)
	issues := map[string]string{}
	if in.ID == uuid.Nil {
		issues["id"] = "required"
	}
	if in.Name == "" {
		issues["name"] = "required"
	}
	if in.Slug == "" {
		issues["slug"] = "required"
	}
	if in.Group == "" {
		issues["group"] = "required"
	}
	if len(issues) > 0 {
		return Space{}, false, httperror.Validation("invalid department", issues)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Space{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id uuid.UUID
	var wasArchived bool
	var oldName string
	err = tx.QueryRow(ctx, `SELECT id, archived_at IS NOT NULL, name FROM spaces WHERE department_id = $1 FOR UPDATE`, in.ID).
		Scan(&id, &wasArchived, &oldName)
	if err == nil {
		// Renames and archiving in HR follow through; everything else is
		// the department's to manage.
		if _, err := tx.Exec(ctx, `
			UPDATE spaces SET name = $2, updated_at = now(),
			       archived_at = CASE WHEN $3 THEN COALESCE(archived_at, now()) ELSE NULL END
			 WHERE id = $1`, id, in.Name, in.Archived); err != nil {
			return Space{}, false, err
		}
		if in.Archived != wasArchived {
			action := "restored"
			if in.Archived {
				action = "archived"
			}
			if err := writeAudit(ctx, tx, nil, "space", id, action, map[string]any{
				"name": in.Name, "department_id": in.ID, "source": "hr_department_hook",
			}); err != nil {
				return Space{}, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return Space{}, false, err
		}
		sp, err := s.GetSpace(ctx, id)
		return sp, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Space{}, false, err
	}

	key, err := freeSpaceKey(ctx, tx, in.Slug)
	if err != nil {
		return Space{}, false, err
	}
	// everyone_level stays NULL: a department space is closed (TASK-24);
	// the department group grant below is what opens it to its members.
	err = tx.QueryRow(ctx, `
		INSERT INTO spaces (organization_id, name, key, department_id, description, position, archived_at)
		VALUES ($1, $2, $3, $4, '', (SELECT COALESCE(max(position), 0) + 1 FROM spaces), CASE WHEN $5 THEN now() END)
		RETURNING id`, orgID, in.Name, key, in.ID, in.Archived).Scan(&id)
	if err != nil {
		return Space{}, false, err
	}
	for i, st := range defaultStatuses {
		if _, err := tx.Exec(ctx, `INSERT INTO statuses (space_id, name, color, category, position) VALUES ($1, $2, $3, $4, $5)`,
			id, st.Name, st.Color, st.Category, i); err != nil {
			return Space{}, false, err
		}
	}
	for i, name := range []string{"To do", "Handoffs", "Done archive"} {
		if _, err := tx.Exec(ctx, `INSERT INTO lists (space_id, name, position) VALUES ($1, $2, $3)`, id, name, i+1); err != nil {
			return Space{}, false, err
		}
	}
	var grantID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO access_grants (space_id, subject_type, subject, subject_key, level)
		VALUES ($1, 'group', $2, $3, 'edit')
		ON CONFLICT (space_id, subject_type, subject_key) WHERE space_id IS NOT NULL
		DO UPDATE SET level = EXCLUDED.level
		RETURNING id`, id, in.Group, subjectKey("group", in.Group)).Scan(&grantID); err != nil {
		return Space{}, false, err
	}
	if err := writeAudit(ctx, tx, nil, "grant", grantID, "granted", map[string]any{
		"space_id": id, "subject_type": "group", "subject": in.Group, "level": "edit", "source": "hr_department_hook",
	}); err != nil {
		return Space{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Space{}, false, err
	}
	sp, err := s.GetSpace(ctx, id)
	return sp, true, err
}

// freeSpaceKey turns a department slug into an unused space key matching
// keyPattern: initials for multi-word slugs ("front-of-house" → FOH), a
// prefix otherwise ("kitchen" → KITC), numbered on collision.
func freeSpaceKey(ctx context.Context, tx pgx.Tx, slug string) (string, error) {
	taken := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT key FROM spaces`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return "", err
		}
		taken[k] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}

	base := keyFromSlug(slug)
	if !taken[base] {
		return base, nil
	}
	for i := 2; i < 100; i++ {
		n := strconv.Itoa(i)
		cand := base
		if len(cand)+len(n) > 6 {
			cand = cand[:6-len(n)]
		}
		cand += n
		if keyPattern.MatchString(cand) && !taken[cand] {
			return cand, nil
		}
	}
	return "", httperror.Conflict("could not find a free space key for " + slug)
}

func keyFromSlug(slug string) string {
	words := strings.FieldsFunc(strings.ToUpper(slug), func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
	key := ""
	if len(words) >= 2 {
		for _, w := range words {
			key += w[:1]
		}
	} else if len(words) == 1 {
		key = words[0]
	}
	// Keys start with a letter; slugs like "2nd-stage" get a D prefix.
	if key == "" || key[0] >= '0' && key[0] <= '9' {
		key = "D" + key
	}
	if len(key) > 6 {
		key = key[:6]
	}
	for len(key) < 2 {
		key += "X"
	}
	return key
}
