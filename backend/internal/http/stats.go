package http

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

type statsMetric struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// internalStatsHandler serves GET /api/internal/stats for the Stats module.
// Authenticated by STATS_PULL_TOKEN; without a token the route 404s.
func internalStatsHandler(token string, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token = strings.TrimSpace(token)
		if token == "" {
			http.NotFound(w, r)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) != 1 {
			httperror.Render(w, r, httperror.New(httperror.CodeUnauthorized, "invalid service token"))
			return
		}
		metrics, err := tasksStats(r.Context(), pool)
		if err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"schema_version": 1,
			"module":         "tasks",
			"generated_at":   time.Now().UTC(),
			"metrics":        metrics,
		})
	}
}

func tasksStats(ctx context.Context, pool *pgxpool.Pool) ([]statsMetric, error) {
	var openTasks, spaces float64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::float8 FROM tasks WHERE archived_at IS NULL`).Scan(&openTasks); err != nil {
		// Older schemas may lack archived_at; fall back to all rows.
		if err := pool.QueryRow(ctx, `SELECT COUNT(*)::float8 FROM tasks`).Scan(&openTasks); err != nil {
			return nil, err
		}
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::float8 FROM spaces WHERE archived_at IS NULL`).Scan(&spaces); err != nil {
		if err := pool.QueryRow(ctx, `SELECT COUNT(*)::float8 FROM spaces`).Scan(&spaces); err != nil {
			return nil, err
		}
	}
	return []statsMetric{
		{Key: "open_tasks", Label: "Open tasks", Value: openTasks, Unit: "count"},
		{Key: "spaces", Label: "Spaces", Value: spaces, Unit: "count"},
	}, nil
}
