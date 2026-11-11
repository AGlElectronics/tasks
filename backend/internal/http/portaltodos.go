package http

import (
	"net/http"
	"strings"

	"github.com/AGlElectronics/tasks/backend/internal/denlink"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Headers Den's backend sends with GET /api/portal/todos. The concept note
// also allows ?issuer=&subject=; headers win when both are present.
const (
	headerDenUserIssuer  = "X-Den-User-Issuer"
	headerDenUserSubject = "X-Den-User-Subject"
)

// portalTodos serves the open tasks assigned to the person Den names.
// Den calls it server to server with the pairing credential, never with
// the person's token.
func portalTodos(den *denlink.Client, svc *tasks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !den.CredentialMatches(r.Context(), bearerToken(r.Header.Get("Authorization"))) {
			httperror.Render(w, r, httperror.New(httperror.CodeUnauthorized, "invalid or revoked credential"))
			return
		}
		issuer := firstNonEmpty(r.Header.Get(headerDenUserIssuer), r.URL.Query().Get("issuer"))
		subject := firstNonEmpty(r.Header.Get(headerDenUserSubject), r.URL.Query().Get("subject"))
		issuer, subject = strings.TrimSpace(issuer), strings.TrimSpace(subject)
		if issuer == "" || subject == "" || len(issuer) > 512 || len(subject) > 512 {
			httperror.Render(w, r, httperror.BadRequest("issuer and subject are required"))
			return
		}
		items, err := svc.PortalTodos(r.Context(), issuer, subject)
		if err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
