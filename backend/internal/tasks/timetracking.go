package tasks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// TimeEntry is time someone logged on a task.
type TimeEntry struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    uuid.UUID  `json:"task_id"`
	UserID    *uuid.UUID `json:"user_id"`
	UserName  string     `json:"user_name"`
	Minutes   int        `json:"minutes"`
	SpentOn   string     `json:"spent_on"`
	Note      string     `json:"note"`
	CreatedAt time.Time  `json:"created_at"`
}

// TimeInput logs time. SpentOn defaults to today; Minutes is 1..1440.
type TimeInput struct {
	Minutes int     `json:"minutes"`
	SpentOn *string `json:"spent_on"`
	Note    string  `json:"note"`
}

const timeSelect = `
	SELECT e.id, e.task_id, e.user_id, COALESCE(u.display_name, 'Former user'), e.minutes,
	       to_char(e.spent_on, 'YYYY-MM-DD'), e.note, e.created_at
	  FROM time_entries e LEFT JOIN users u ON u.id = e.user_id`

func scanTimeEntry(row pgx.Row) (TimeEntry, error) {
	var e TimeEntry
	err := row.Scan(&e.ID, &e.TaskID, &e.UserID, &e.UserName, &e.Minutes, &e.SpentOn, &e.Note, &e.CreatedAt)
	return e, err
}

// TimeEntries lists a task's time entries, newest day first (at most 500).
func (s *Service) TimeEntries(ctx context.Context, taskID uuid.UUID) ([]TimeEntry, error) {
	rows, err := s.pool.Query(ctx, timeSelect+` WHERE e.task_id = $1 ORDER BY e.spent_on DESC, e.created_at DESC LIMIT 500`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		e, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LogTime records time spent by the actor.
func (s *Service) LogTime(ctx context.Context, actor, taskID uuid.UUID, in TimeInput) (TimeEntry, error) {
	issues := map[string]string{}
	if in.Minutes < 1 || in.Minutes > 1440 {
		issues["minutes"] = "1 to 1440 (one day)"
	}
	if in.SpentOn != nil && !validDate(in.SpentOn) {
		issues["spent_on"] = "must be YYYY-MM-DD"
	}
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) > 1000 {
		issues["note"] = "too long"
	}
	if len(issues) > 0 {
		return TimeEntry{}, httperror.Validation("invalid time entry", issues)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TimeEntry{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO time_entries (task_id, user_id, minutes, spent_on, note)
		VALUES ($1, $2, $3, COALESCE($4::date, CURRENT_DATE), $5) RETURNING id`,
		taskID, actor, in.Minutes, in.SpentOn, in.Note).Scan(&id); err != nil {
		return TimeEntry{}, err
	}
	if err := addEvent(ctx, tx, taskID, actor, "time_logged", map[string]any{"minutes": in.Minutes}); err != nil {
		return TimeEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TimeEntry{}, err
	}
	return scanTimeEntry(s.pool.QueryRow(ctx, timeSelect+` WHERE e.id = $1`, id))
}

// GetTimeEntry loads one entry or a typed 404.
func (s *Service) GetTimeEntry(ctx context.Context, id uuid.UUID) (TimeEntry, error) {
	e, err := scanTimeEntry(s.pool.QueryRow(ctx, timeSelect+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return TimeEntry{}, httperror.NotFound("time entry")
	}
	return e, err
}

// DeleteTimeEntry removes an entry (the handler checks who may).
func (s *Service) DeleteTimeEntry(ctx context.Context, actor uuid.UUID, e TimeEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM time_entries WHERE id = $1`, e.ID); err != nil {
		return err
	}
	if err := addEvent(ctx, tx, e.TaskID, actor, "time_removed", map[string]any{"minutes": e.Minutes, "user": e.UserName}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TimeReportRow is one person's total in a list's time report.
type TimeReportRow struct {
	UserID   *uuid.UUID `json:"user_id"`
	UserName string     `json:"user_name"`
	Minutes  int        `json:"minutes"`
}

// ListTimeReport sums logged time per person over a list's tasks (subtasks
// included) between from and to (inclusive, either may be nil).
func (s *Service) ListTimeReport(ctx context.Context, listID uuid.UUID, from, to *string) ([]TimeReportRow, int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.user_id, COALESCE(u.display_name, 'Former user'), sum(e.minutes)::int
		  FROM time_entries e JOIN tasks t ON t.id = e.task_id LEFT JOIN users u ON u.id = e.user_id
		 WHERE t.list_id = $1 AND t.deleted_at IS NULL
		   AND ($2::date IS NULL OR e.spent_on >= $2::date) AND ($3::date IS NULL OR e.spent_on <= $3::date)
		 GROUP BY e.user_id, u.display_name ORDER BY 3 DESC`, listID, from, to)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []TimeReportRow{}
	total := 0
	for rows.Next() {
		var r TimeReportRow
		if err := rows.Scan(&r.UserID, &r.UserName, &r.Minutes); err != nil {
			return nil, 0, err
		}
		total += r.Minutes
		out = append(out, r)
	}
	return out, total, rows.Err()
}
