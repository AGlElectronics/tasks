package http

import (
	"net/http"

	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// portalTodos serves the open tasks assigned to the person Den names.
// Den's backend calls it with the pairing credential, never with the
// person's token. RequireDen checks that credential and reads the person
// from X-Den-User-Issuer and X-Den-User-Subject. Den does not send the
// person in the query string.
func portalTodos(den *denlink.Client, svc *tasks.Service) http.HandlerFunc {
	return den.RequireDen(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := denlink.PortalCallerFrom(r.Context())
		if !ok {
			httperror.Render(w, r, httperror.BadRequest("issuer and subject are required"))
			return
		}
		items, err := svc.PortalTodos(r.Context(), caller.Issuer, caller.Subject)
		if err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}))
}
