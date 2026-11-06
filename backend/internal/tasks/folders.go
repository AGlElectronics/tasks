package tasks

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Folder groups lists inside a space (TASK-9). Folders are one level deep:
// a space has folders, folders have lists, and that is the whole hierarchy
// — deep nesting is what made Vikunja unreadable.
type Folder struct {
	ID       uuid.UUID `json:"id"`
	SpaceID  uuid.UUID `json:"space_id"`
	Name     string    `json:"name"`
	Position int       `json:"position"`
}

// SpaceFolders lists a space's folders in order.
func (s *Service) SpaceFolders(ctx context.Context, spaceID uuid.UUID) ([]Folder, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, space_id, name, position FROM folders WHERE space_id = $1 ORDER BY position, created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.SpaceID, &f.Name, &f.Position); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FolderInput creates or updates a folder.
type FolderInput struct {
	Name     *string `json:"name"`
	Position *int    `json:"position"`
}

// CreateFolder adds a folder at the end of a space.
func (s *Service) CreateFolder(ctx context.Context, spaceID uuid.UUID, in FolderInput) (Folder, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return Folder{}, httperror.Validation("invalid folder", map[string]string{"name": "required"})
	}
	f := Folder{SpaceID: spaceID, Name: strings.TrimSpace(*in.Name)}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO folders (space_id, name, position)
		VALUES ($1, $2, (SELECT COALESCE(max(position), 0) + 1 FROM folders WHERE space_id = $1))
		RETURNING id, position`, spaceID, f.Name).Scan(&f.ID, &f.Position)
	return f, err
}

// FolderSpace returns the folder's space for access checks.
func (s *Service) FolderSpace(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var spaceID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT space_id FROM folders WHERE id = $1`, id).Scan(&spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.NotFound("folder")
	}
	return spaceID, err
}

// UpdateFolder applies the non-nil fields.
func (s *Service) UpdateFolder(ctx context.Context, id uuid.UUID, in FolderInput) (Folder, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return Folder{}, httperror.Validation("invalid folder", map[string]string{"name": "required"})
	}
	var name *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		name = &n
	}
	var f Folder
	err := s.pool.QueryRow(ctx, `
		UPDATE folders SET name = COALESCE($2, name), position = COALESCE($3, position)
		 WHERE id = $1 RETURNING id, space_id, name, position`, id, name, in.Position).
		Scan(&f.ID, &f.SpaceID, &f.Name, &f.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return Folder{}, httperror.NotFound("folder")
	}
	return f, err
}

// DeleteFolder removes a folder; its lists drop back to the space root
// (folder_id is SET NULL), nothing else is lost.
func (s *Service) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM folders WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("folder")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Favourites (TASK-11)
// ----------------------------------------------------------------------------

// Favorite pins a space or a list to the top of the user's sidebar.
type Favorite struct {
	SpaceID *uuid.UUID `json:"space_id,omitempty"`
	ListID  *uuid.UUID `json:"list_id,omitempty"`
}

// Favorites lists the user's favourites.
func (s *Service) Favorites(ctx context.Context, userID uuid.UUID) ([]Favorite, error) {
	rows, err := s.pool.Query(ctx, `SELECT space_id, list_id FROM favorites WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.SpaceID, &f.ListID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFavorite pins a space or a list.
func (s *Service) AddFavorite(ctx context.Context, userID uuid.UUID, f Favorite) error {
	if (f.SpaceID == nil) == (f.ListID == nil) {
		return httperror.Validation("pick a space or a list", nil)
	}
	var err error
	if f.SpaceID != nil {
		_, err = s.pool.Exec(ctx, `INSERT INTO favorites (user_id, space_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, *f.SpaceID)
	} else {
		_, err = s.pool.Exec(ctx, `INSERT INTO favorites (user_id, list_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, *f.ListID)
	}
	if err != nil && strings.Contains(err.Error(), "SQLSTATE 23503") {
		return httperror.NotFound("favourite target")
	}
	return err
}

// RemoveFavorite unpins a space or a list.
func (s *Service) RemoveFavorite(ctx context.Context, userID uuid.UUID, f Favorite) error {
	if (f.SpaceID == nil) == (f.ListID == nil) {
		return httperror.Validation("pick a space or a list", nil)
	}
	if f.SpaceID != nil {
		_, err := s.pool.Exec(ctx, `DELETE FROM favorites WHERE user_id = $1 AND space_id = $2`, userID, *f.SpaceID)
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM favorites WHERE user_id = $1 AND list_id = $2`, userID, *f.ListID)
	return err
}
