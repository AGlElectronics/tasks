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

// Attachment is file metadata (TASK-7); the bytes live in S3 under S3Key
// and are always streamed through the backend so access checks apply.
type Attachment struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	Filename    string     `json:"filename"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	S3Key       string     `json:"-"`
	UploadedBy  *uuid.UUID `json:"uploaded_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Attachments lists a task's attachments, oldest first.
func (s *Service) Attachments(ctx context.Context, taskID uuid.UUID) ([]Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, filename, content_type, size_bytes, s3_key, uploaded_by, created_at
		  FROM attachments WHERE task_id = $1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.S3Key, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAttachment loads one attachment's metadata.
func (s *Service) GetAttachment(ctx context.Context, id uuid.UUID) (Attachment, error) {
	var a Attachment
	err := s.pool.QueryRow(ctx, `
		SELECT id, task_id, filename, content_type, size_bytes, s3_key, uploaded_by, created_at
		  FROM attachments WHERE id = $1`, id).
		Scan(&a.ID, &a.TaskID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.S3Key, &a.UploadedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, httperror.NotFound("attachment")
	}
	return a, err
}

// AddAttachment records an uploaded file after the bytes made it to S3.
func (s *Service) AddAttachment(ctx context.Context, actor, taskID uuid.UUID, filename, contentType, s3Key string, size int64) (Attachment, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "file"
	}
	if len(filename) > 255 {
		filename = filename[len(filename)-255:]
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	a := Attachment{TaskID: taskID, Filename: filename, ContentType: contentType, SizeBytes: size, S3Key: s3Key, UploadedBy: &actor}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Attachment{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	err = tx.QueryRow(ctx, `
		INSERT INTO attachments (task_id, filename, content_type, size_bytes, s3_key, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		taskID, filename, contentType, size, s3Key, actor).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "SQLSTATE 23503") {
			return Attachment{}, httperror.NotFound("task")
		}
		return Attachment{}, err
	}
	if err := addEvent(ctx, tx, taskID, actor, "attachment", map[string]any{"filename": filename}); err != nil {
		return Attachment{}, err
	}
	return a, tx.Commit(ctx)
}

// DeleteAttachment removes the metadata row; the handler removes the S3
// object afterwards (an orphaned object is harmless, a dangling row is not).
func (s *Service) DeleteAttachment(ctx context.Context, actor, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var filename string
	var taskID uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM attachments WHERE id = $1 RETURNING filename, task_id`, id).Scan(&filename, &taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("attachment")
	}
	if err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, &actor, "attachment", id, "deleted", map[string]any{"filename": filename, "task_id": taskID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
