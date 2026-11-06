package tasks

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Watchers, mentions and the Inbox (TASK-5). A watcher follows a task
// without being assigned; watchers and assignees get Inbox notifications on
// status changes and comments, mentioned people get one for the mention.

// Watchers lists the people following a task.
func (s *Service) Watchers(ctx context.Context, taskID uuid.UUID) ([]Person, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.display_name FROM task_watchers w JOIN users u ON u.id = w.user_id
		 WHERE w.task_id = $1 ORDER BY u.display_name`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Watch subscribes the user to a task's notifications.
func (s *Service) Watch(ctx context.Context, userID, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO task_watchers (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, taskID, userID)
	return err
}

// Unwatch unsubscribes the user.
func (s *Service) Unwatch(ctx context.Context, userID, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM task_watchers WHERE task_id = $1 AND user_id = $2`, taskID, userID)
	return err
}

// IsWatching reports whether the user watches the task. Watching also opens
// the task at view level, like assignment opens it at edit level.
func (s *Service) IsWatching(ctx context.Context, userID, taskID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM task_watchers WHERE task_id = $1 AND user_id = $2)`, taskID, userID).Scan(&ok)
	return ok, err
}

// notify writes one Inbox notification per recipient, skipping the actor
// (people should not be notified about their own actions) and duplicates.
// It runs inside the transaction of the change itself, so a rolled-back
// change never notifies anyone.
func (s *Service) notify(ctx context.Context, tx pgx.Tx, taskID, actor uuid.UUID, kind, message string, userIDs []uuid.UUID) error {
	seen := map[uuid.UUID]bool{actor: true}
	for _, uid := range userIDs {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (user_id, task_id, task_key, task_title, actor_id, kind, message)
			SELECT $1, t.id, sp.key || '-' || t.number, t.title, $3, $4, $5
			  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $2`,
			uid, taskID, actor, kind, message); err != nil {
			return err
		}
	}
	return nil
}

// followers are the task's watchers plus its assignees.
func (s *Service) followers(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id FROM task_watchers WHERE task_id = $1
		UNION SELECT user_id FROM task_assignees WHERE task_id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Service) notifyFollowers(ctx context.Context, tx pgx.Tx, taskID, actor uuid.UUID, kind, message string) error {
	return s.notifyFollowersExcept(ctx, tx, taskID, actor, kind, message, nil)
}

func (s *Service) notifyFollowersExcept(ctx context.Context, tx pgx.Tx, taskID, actor uuid.UUID, kind, message string, except []uuid.UUID) error {
	ids, err := s.followers(ctx, tx, taskID)
	if err != nil {
		return err
	}
	skip := map[uuid.UUID]bool{}
	for _, id := range except {
		skip[id] = true
	}
	var to []uuid.UUID
	for _, id := range ids {
		if !skip[id] {
			to = append(to, id)
		}
	}
	return s.notify(ctx, tx, taskID, actor, kind, message, to)
}

// Notification is one Inbox row.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    *uuid.UUID `json:"task_id"`
	TaskKey   string     `json:"task_key"`
	TaskTitle string     `json:"task_title"`
	ActorName string     `json:"actor_name"`
	Kind      string     `json:"kind"`
	Message   string     `json:"message"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// Inbox returns the user's notifications, newest first.
func (s *Service) Inbox(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]Notification, error) {
	limit = ClampLimit(limit)
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.task_id, n.task_key, n.task_title, COALESCE(u.display_name, 'Someone'),
		       n.kind, n.message, n.read_at, n.created_at
		  FROM notifications n LEFT JOIN users u ON u.id = n.actor_id
		 WHERE n.user_id = $1 AND ($2::bool = false OR n.read_at IS NULL)
		 ORDER BY n.created_at DESC LIMIT $3`, userID, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.TaskID, &n.TaskKey, &n.TaskTitle, &n.ActorName, &n.Kind, &n.Message, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount is the number shown on the Inbox badge.
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// MarkRead marks the given notifications read; with no ids it marks all of
// the user's notifications read.
func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	var err error
	if len(ids) == 0 {
		_, err = s.pool.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND id = ANY($2) AND read_at IS NULL`, userID, ids)
	}
	return err
}
