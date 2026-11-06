package http

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// internalDepartmentsHandler serves POST /api/internal/departments for HR
// (TASK-25): called when a department is created or renamed, it ensures the
// department's one space exists and tracks its name. Authenticated by the
// shared TASKS_SERVICE_TOKEN bearer token; without a configured token the
// route does not exist (404), so the feature is opt-in per deployment.
func internalDepartmentsHandler(cfg *config.Config, svc *tasks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(cfg.TasksServiceToken)
		if token == "" {
			http.NotFound(w, r)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) != 1 {
			httperror.Render(w, r, httperror.New(httperror.CodeUnauthorized, "invalid service token"))
			return
		}
		var in tasks.DepartmentSpaceInput
		if err := decodeJSON(r, &in); err != nil {
			httperror.Render(w, r, err)
			return
		}
		orgID, err := svc.DefaultOrganization(r.Context())
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		sp, created, err := svc.EnsureDepartmentSpace(r.Context(), orgID, in)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, sp)
	}
}
