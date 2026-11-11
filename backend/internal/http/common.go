package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/authbase"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// writeJSON serializes body with the standard content-type. Errors on the
// writer path (e.g. client disconnected) are intentionally swallowed — the
// standard access log already records the status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

// decodeJSON strict-decodes the request body into dst and returns a
// typed BadRequest on malformed input so handlers can hand the error
// straight to httperror.Render.
func decodeJSON(r *http.Request, dst any) error {
	return decode(r, dst, true)
}

// decodeJSONLenient ignores unknown fields. Only for service-to-service
// contracts where the caller may add fields before Tasks knows them (HR's
// department hook).
func decodeJSONLenient(r *http.Request, dst any) error {
	return decode(r, dst, false)
}

// decode reads one JSON value with authbase.DecodeJSON under the route's
// body cap. The ResponseWriter is nil: the router's body limit already
// wrapped r.Body with the real one, which closes the connection on overflow.
func decode(r *http.Request, dst any, strict bool) error {
	err := authbase.DecodeJSON(nil, r, dst, authbase.DecodeOptions{MaxBytes: bodyLimit(r), DisallowUnknownFields: strict})
	var de *authbase.DecodeError
	if errors.As(err, &de) {
		return httperror.New(httperror.Code(de.Code), de.Message)
	}
	return err
}

// bodyLimit is the request body cap: 1 MiB, more for attachment uploads
// (TASK-7) and imports, which carry files.
func bodyLimit(r *http.Request) int64 {
	if r.Method == http.MethodPost {
		switch {
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			return maxAttachmentSize + authbase.DefaultMaxBodyBytes
		case strings.HasSuffix(r.URL.Path, "/import"):
			// JSON-escaping can grow the 5 MB file a little.
			return 2*tasks.MaxImportBytes + authbase.DefaultMaxBodyBytes
		}
	}
	return authbase.DefaultMaxBodyBytes
}

// chiParam returns a raw chi URL parameter.
func chiParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// parseUUIDParam reads a chi URL parameter and parses it as a UUID. The
// `name` is used verbatim in the error message, so pass a human-friendly
// label like "organization" rather than the raw URL fragment name.
func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httperror.BadRequest("invalid " + name + " id: " + raw)
	}
	return id, nil
}
