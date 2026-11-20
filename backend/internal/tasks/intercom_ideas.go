package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

const (
	intercomSpaceName = "Intercom"
	intercomSpaceKey  = "ICM"
	intercomListName  = "Ideas"
)

// IntercomIdeaInput is what Intercom sends to POST /api/internal/ideas.
type IntercomIdeaInput struct {
	Title           string `json:"title"`
	Body            string `json:"body"`
	SourceUserName  string `json:"source_user_name"`
	SourceChatTitle string `json:"source_chat_title"`
}

// IntercomIdeaResult is the created task plus where it landed.
type IntercomIdeaResult struct {
	Task      Task   `json:"task"`
	SpaceID   string `json:"space_id"`
	SpaceKey  string `json:"space_key"`
	SpaceName string `json:"space_name"`
	ListID    string `json:"list_id"`
	ListName  string `json:"list_name"`
	TaskKey   string `json:"task_key"`
}

// CreateIntercomIdea ensures the Intercom / Ideas list exists and creates a
// task there from a Telegram /idea capture.
func (s *Service) CreateIntercomIdea(ctx context.Context, in IntercomIdeaInput) (IntercomIdeaResult, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	in.SourceUserName = strings.TrimSpace(in.SourceUserName)
	in.SourceChatTitle = strings.TrimSpace(in.SourceChatTitle)
	if in.Body == "" && in.Title == "" {
		return IntercomIdeaResult{}, httperror.Validation("idea required", map[string]string{"body": "required"})
	}
	if in.Title == "" {
		in.Title = ideaTitle(in.Body)
	}
	if utf8.RuneCountInString(in.Title) > maxTitleLen {
		in.Title = string([]rune(in.Title)[:maxTitleLen])
	}

	orgID, err := s.DefaultOrganization(ctx)
	if err != nil {
		return IntercomIdeaResult{}, err
	}
	spaceID, listID, err := s.ensureIntercomIdeasList(ctx, orgID)
	if err != nil {
		return IntercomIdeaResult{}, err
	}
	actor, err := s.firstUser(ctx)
	if err != nil {
		return IntercomIdeaResult{}, err
	}

	desc := in.Body
	meta := []string{}
	if in.SourceUserName != "" {
		meta = append(meta, "From: "+in.SourceUserName)
	}
	if in.SourceChatTitle != "" {
		meta = append(meta, "Chat: "+in.SourceChatTitle)
	}
	if len(meta) > 0 {
		desc = strings.Join(meta, "\n") + "\n\n" + in.Body
	}

	task, err := s.CreateTask(ctx, actor, TaskInput{
		ListID:      &listID,
		Title:       in.Title,
		Description: desc,
	})
	if err != nil {
		return IntercomIdeaResult{}, err
	}

	var spaceKey, spaceName string
	_ = s.pool.QueryRow(ctx, `SELECT key, name FROM spaces WHERE id = $1`, spaceID).Scan(&spaceKey, &spaceName)
	taskKey := fmt.Sprintf("%s-%d", spaceKey, task.Number)
	return IntercomIdeaResult{
		Task:      task,
		SpaceID:   spaceID.String(),
		SpaceKey:  spaceKey,
		SpaceName: spaceName,
		ListID:    listID.String(),
		ListName:  intercomListName,
		TaskKey:   taskKey,
	}, nil
}

func (s *Service) ensureIntercomIdeasList(ctx context.Context, orgID uuid.UUID) (spaceID, listID uuid.UUID, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `SELECT id FROM spaces WHERE key = $1 FOR UPDATE`, intercomSpaceKey).Scan(&spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO spaces (organization_id, name, key, color, description, everyone_level, position)
			VALUES ($1, $2, $3, '#7ec8a3', 'Ideas captured from Intercom Telegram /idea', 'view',
			        (SELECT COALESCE(max(position), 0) + 1 FROM spaces))
			RETURNING id`, orgID, intercomSpaceName, intercomSpaceKey).Scan(&spaceID)
		if err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		for i, st := range defaultStatuses {
			if _, err := tx.Exec(ctx, `INSERT INTO statuses (space_id, name, color, category, position) VALUES ($1, $2, $3, $4, $5)`,
				spaceID, st.Name, st.Color, st.Category, i); err != nil {
				return uuid.Nil, uuid.Nil, err
			}
		}
		if err := writeAudit(ctx, tx, nil, "space", spaceID, "created", map[string]any{
			"name": intercomSpaceName, "key": intercomSpaceKey, "source": "intercom_ideas",
		}); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
	} else if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	err = tx.QueryRow(ctx, `
		SELECT id FROM lists
		 WHERE space_id = $1 AND name = $2 AND archived_at IS NULL
		 ORDER BY position LIMIT 1`, spaceID, intercomListName).Scan(&listID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO lists (space_id, name, position)
			VALUES ($1, $2, (SELECT COALESCE(max(position), 0) + 1 FROM lists WHERE space_id = $1))
			RETURNING id`, spaceID, intercomListName).Scan(&listID)
		if err != nil {
			return uuid.Nil, uuid.Nil, err
		}
	} else if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return spaceID, listID, nil
}

func (s *Service) firstUser(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM users ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.Conflict("Tasks has no users yet; sign in once before capturing ideas")
	}
	return id, err
}

func ideaTitle(body string) string {
	line := strings.TrimSpace(strings.Split(body, "\n")[0])
	if line == "" {
		return "Idea"
	}
	runes := []rune(line)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return line
}
