package tasks

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Tag is a label scoped to a space (TASK-4).
type Tag struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
}

const maxTagLen = 50

// SpaceTags lists a space's tags alphabetically.
func (s *Service) SpaceTags(ctx context.Context, spaceID uuid.UUID) ([]Tag, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, color FROM tags WHERE space_id = $1 ORDER BY lower(name)`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TagInput creates or updates a tag.
type TagInput struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

func (in *TagInput) validate(create bool) error {
	issues := map[string]string{}
	if create || in.Name != nil {
		if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
			issues["name"] = "required"
		} else if len(strings.TrimSpace(*in.Name)) > maxTagLen {
			issues["name"] = "too long"
		}
	}
	if in.Color != nil && !colorPattern.MatchString(*in.Color) {
		issues["color"] = "must look like #1a2b3c"
	}
	if len(issues) > 0 {
		return httperror.Validation("invalid tag", issues)
	}
	return nil
}

// CreateTag adds a tag to a space.
func (s *Service) CreateTag(ctx context.Context, spaceID uuid.UUID, in TagInput) (Tag, error) {
	if err := in.validate(true); err != nil {
		return Tag{}, err
	}
	color := "#6366f1"
	if in.Color != nil {
		color = *in.Color
	}
	t := Tag{Name: strings.TrimSpace(*in.Name), Color: color}
	err := s.pool.QueryRow(ctx, `INSERT INTO tags (space_id, name, color) VALUES ($1, $2, $3) RETURNING id`,
		spaceID, t.Name, t.Color).Scan(&t.ID)
	if isUniqueViolation(err) {
		return Tag{}, httperror.Conflict("this space already has a tag with that name")
	}
	return t, err
}

// UpdateTag renames or recolours a tag.
func (s *Service) UpdateTag(ctx context.Context, spaceID, id uuid.UUID, in TagInput) (Tag, error) {
	if err := in.validate(false); err != nil {
		return Tag{}, err
	}
	var name *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		name = &n
	}
	var t Tag
	err := s.pool.QueryRow(ctx, `
		UPDATE tags SET name = COALESCE($3, name), color = COALESCE($4, color)
		 WHERE id = $1 AND space_id = $2 RETURNING id, name, color`,
		id, spaceID, name, in.Color).Scan(&t.ID, &t.Name, &t.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, httperror.NotFound("tag")
	}
	if isUniqueViolation(err) {
		return Tag{}, httperror.Conflict("this space already has a tag with that name")
	}
	return t, err
}

// DeleteTag removes a tag from the space and from every task that has it.
func (s *Service) DeleteTag(ctx context.Context, spaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tags WHERE id = $1 AND space_id = $2`, id, spaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("tag")
	}
	return nil
}
