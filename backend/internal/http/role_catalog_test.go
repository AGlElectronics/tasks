package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/config"
)

// TestRoleCatalogContract: GET /api/role-catalog needs HR_SERVICE_TOKEN or
// a sign-in, and carries the 1.0 fields. There is no OIDC verifier here, so
// only requests without a bearer token are checked for 401.
func TestRoleCatalogContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.toml")
	if err := os.WriteFile(path, []byte(accessTOML+"\n[[bindings]]\ngroup = \"/Tasks/Admins\"\nrole = \"organization_admin\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	access, err := auth.LoadAccessConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AuthMode: config.AuthModeOIDC, HRServiceToken: "hr-secret"}
	router := NewRouter(Deps{Config: cfg, Logger: zerolog.Nop(), Access: access}, "test")
	get := func(header http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/role-catalog", nil)
		for k, v := range header {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for name, h := range map[string]http.Header{
		"no token":   {},
		"dev header": {"X-Dev-User": {"dev-admin"}},
	} {
		if rec := get(h); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, rec.Code, rec.Body.String())
		}
	}

	rec := get(http.Header{"Authorization": {"Bearer hr-secret"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("HR token: %d %s", rec.Code, rec.Body.String())
	}
	var c roleCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Module != "tasks" || c.Revision == "" || rec.Header().Get("ETag") != `"`+c.Revision+`"` {
		t.Errorf("catalog header fields: %+v etag %q", c, rec.Header().Get("ETag"))
	}
	byGroup := map[string]roleCatalogEntry{}
	for _, r := range c.Roles {
		byGroup[r.Group] = r
	}
	admin, creator, path2 := byGroup["tasks-admin"], byGroup["tasks-creator"], byGroup["tasks/admins"]
	if !admin.Privileged || admin.Dynamic || !admin.Assignable || admin.Key != "organization_admin" {
		t.Errorf("organization_admin entry: %+v", admin)
	}
	if creator.Privileged || creator.Key != "space_creator" {
		t.Errorf("space_creator entry: %+v", creator)
	}
	if path2.GroupDisplay != "/Tasks/Admins" || path2.Key != "organization_admin" {
		t.Errorf("group stored normalised, shown as typed: %+v", path2)
	}

	if rec := get(http.Header{"Authorization": {"Bearer hr-secret"}, "If-None-Match": {rec.Header().Get("ETag")}}); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match with the current revision: %d, want 304", rec.Code)
	}
}
