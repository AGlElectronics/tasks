package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/denlink"
)

// TestDenPairingGate checks the routing of DEN_MODE=required on the real
// router: the state endpoint is public, the Den admin routes still go
// through authentication, and other API routes answer 503. No database is
// needed: OIDC without a verifier refuses every request before a handler runs.
func TestDenPairingGate(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModeOIDC, Den: denlink.Config{Mode: denlink.ModeRequired}}
	den := denlink.New(nil, denlink.Config{Slug: "test", Mode: cfg.Den.Mode, AppEnv: "development"}, zerolog.Nop())
	router := NewRouter(Deps{Config: cfg, Logger: zerolog.Nop(), Den: den}, "test")
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	for _, path := range []string{"/api/den/state", "/api/v1/den/state"} {
		rec := get(path)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"mode":"required","paired":false,"revoked":false}` {
			t.Errorf("GET %s = %d %s", path, rec.Code, got)
		}
		if rec.Header().Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: API security headers missing: %v", path, rec.Header())
		}
	}

	rec := get("/api/v1/spaces")
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Error.Code != "den_pairing_required" {
		t.Errorf("GET /api/v1/spaces = %d %s, want 503 den_pairing_required", rec.Code, rec.Body.String())
	}

	rec = get("/api/v1/den/")
	if rec.Code == http.StatusOK || rec.Code == http.StatusNotFound || rec.Code == http.StatusServiceUnavailable {
		t.Errorf("GET /api/v1/den/ = %d %s, want it refused by authentication", rec.Code, rec.Body.String())
	}

	rec = get("/api/role-catalog")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/role-catalog without a token = %d %s, want 401", rec.Code, rec.Body.String())
	}
}

// TestDenRevokedState: a pairing Den rejected (den_link.revoked_at) is
// reported by the public state and the admin status, and the cached bundle
// (organisation and module settings) keeps applying.
func TestDenRevokedState(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO den_link (den_base_url, credential, bundle, revoked_at, last_error)
		VALUES ('http://den', 'x', $1, now(), 'Den rejected this module''s credential')`,
		`{"brand":{"organization_name":"Con"},"settings":{"data":{}},"module_settings":{"retention":{"trash_days":7}}}`); err != nil {
		t.Fatal(err)
	}
	den := denlink.New(h.pool, denlink.Config{Slug: "tasks", AppEnv: "development"}, zerolog.Nop())
	if err := den.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	h.rebuild(func(d *Deps) { d.Den = den })

	rec, body := h.do(as{user: "anyone"}, "GET", "/api/den/state", nil)
	if rec.Code != http.StatusOK || body["revoked"] != true || body["paired"] != true {
		t.Fatalf("state: %d %s", rec.Code, rec.Body.String())
	}
	status := h.must(as{user: "admin", groups: "tasks-admin"}, "GET", "/api/v1/den/", nil, 200)
	if status["revoked"] != true || status["revoked_at"] == nil {
		t.Fatalf("admin status should report the revoked credential: %v", status)
	}
	settings := h.must(as{user: "admin", groups: "tasks-admin"}, "GET", "/api/v1/settings", nil, 200)
	if src := settings["sources"].(map[string]any)["retention.trash_days"]; src != "den" {
		t.Fatalf("module settings from the cached bundle should still apply, got source %v", src)
	}
}

// TestDevHeadersOnlyInDevelopment: outside APP_ENV=development the CORS
// preflight does not allow X-Dev-User, and the header never authenticates.
func TestDevHeadersOnlyInDevelopment(t *testing.T) {
	preflight := func(cfg *config.Config) string {
		router := NewRouter(Deps{Config: cfg, Logger: zerolog.Nop()}, "test")
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
		req.Header.Set("Origin", "http://localhost:3009")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "x-dev-user")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	}
	origins := []string{"http://localhost:3009"}
	if got := preflight(&config.Config{AuthMode: config.AuthModeOIDC, AppEnv: "production", CORSAllowedOrigins: origins}); strings.Contains(got, "x-dev-user") {
		t.Errorf("production preflight allows X-Dev-User: %q", got)
	}
	if got := preflight(&config.Config{AuthMode: config.AuthModeDev, AppEnv: "development", CORSAllowedOrigins: origins}); !strings.Contains(got, "x-dev-user") {
		t.Errorf("development preflight must allow X-Dev-User, got %q", got)
	}

	router := NewRouter(Deps{Config: &config.Config{AuthMode: config.AuthModeOIDC, AppEnv: "production"}, Logger: zerolog.Nop()}, "test")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("X-Dev-User", "dev-admin")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("X-Dev-User in production = %d %s, want 401", rec.Code, rec.Body.String())
	}
}
