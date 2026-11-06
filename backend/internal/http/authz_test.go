package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/brand"
	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/migrations"
)

// Authorization tests against a real Postgres (TASK-2). They run only when
// TEST_DATABASE_URL points at a disposable database, e.g.
//
//	TEST_DATABASE_URL=postgres://tasks:tasks@localhost:5440/tasks_test?sslmode=disable go test ./internal/http
//
// The schema is dropped and re-migrated on every run.
const accessTOML = `
organization = "demo-org"

[roles.organization_admin]
description = "Admin"
permissions = ["space.manage", "space.create", "settings.manage"]

[roles.space_creator]
description = "Creator"
permissions = ["space.create"]

[[bindings]]
group = "tasks-admin"
role = "organization_admin"

[[bindings]]
group = "tasks-creator"
role = "space_creator"
`

type harness struct {
	t      *testing.T
	router http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping authorization tests")
	}
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("goose_db_version")
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organizations (slug, display_name) VALUES ('demo-org', 'Demo Org') ON CONFLICT (slug) DO NOTHING`); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	tomlPath := filepath.Join(t.TempDir(), "access.toml")
	if err := os.WriteFile(tomlPath, []byte(accessTOML), 0o600); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	access, err := auth.LoadAccessConfig(tomlPath)
	if err != nil {
		t.Fatalf("access config: %v", err)
	}

	cfg := &config.Config{
		AppEnv:   "development",
		AuthMode: config.AuthModeDev,
	}
	router := NewRouter(Deps{
		Config: cfg,
		Pool:   pool,
		Logger: zerolog.Nop(),
		Brand:  brand.New(cfg),
		Access: access,
	}, "test")
	return &harness{t: t, router: router}
}

// as is a request principal: dev user name plus simulated IdP groups.
type as struct {
	user   string
	groups string // comma-separated; "" means no groups
}

func (h *harness) do(p as, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	h.t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dev-User", p.user)
	req.Header.Set("X-Dev-Groups", p.groups)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func (h *harness) must(p as, method, path string, body any, wantStatus int) map[string]any {
	h.t.Helper()
	rec, out := h.do(p, method, path, body)
	if rec.Code != wantStatus {
		h.t.Fatalf("%s %s as %q(%s): got %d want %d — body %s", method, path, p.user, p.groups, rec.Code, wantStatus, rec.Body.String())
	}
	return out
}

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func TestAuthorization(t *testing.T) {
	h := newHarness(t)
	admin := as{user: "admin", groups: "tasks-admin"}
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob", groups: ""}
	carol := as{user: "carol", groups: ""}

	// Provision bob and carol (first request creates the users row).
	h.must(bob, "GET", "/api/v1/me", nil, 200)
	h.must(carol, "GET", "/api/v1/me", nil, 200)

	// Alice (space_creator) creates a space. New spaces start closed.
	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Security", "key": "SEC"}, 201)
	spaceID := str(sp, "id")
	if sp["everyone_level"] != nil {
		t.Fatalf("new space should start closed, got everyone_level=%v", sp["everyone_level"])
	}
	lists := sp["lists"].([]any)
	listID := str(lists[0].(map[string]any), "id")

	// Bob (no groups, no grants) cannot create spaces, see the space, its
	// list, or create tasks in it (TASK-27, closed space).
	h.must(bob, "POST", "/api/v1/spaces", map[string]any{"name": "X", "key": "XX"}, 403)
	h.must(bob, "GET", "/api/v1/spaces/"+spaceID, nil, 404)
	h.must(bob, "GET", "/api/v1/lists/"+listID+"/tasks", nil, 404)
	h.must(bob, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "nope"}, 404)

	// The org admin sees everything.
	h.must(admin, "GET", "/api/v1/spaces/"+spaceID, nil, 200)

	// Alice creates a task; bob cannot read it.
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "harden the vault"}, 201)
	taskKey := str(task, "key")
	h.must(bob, "GET", "/api/v1/tasks/"+taskKey, nil, 404)

	// Grant bob view: read works, writes stay forbidden.
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "user", "subject": bobID, "level": "view"}, 200)
	h.must(bob, "GET", "/api/v1/tasks/"+taskKey, nil, 200)
	h.must(bob, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "nope"}, 403)
	h.must(bob, "POST", "/api/v1/tasks/"+taskKey+"/comments", map[string]any{"body": "hi"}, 403)

	// Raise to edit: bob can write but not manage or delete others' tasks
	// (TASK-28).
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "user", "subject": bobID, "level": "edit"}, 200)
	bobTask := h.must(bob, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "bob's"}, 201)
	h.must(bob, "DELETE", "/api/v1/tasks/"+taskKey, nil, 403)
	h.must(bob, "DELETE", "/api/v1/tasks/"+str(bobTask, "key"), nil, 204) // own task
	h.must(bob, "PATCH", "/api/v1/spaces/"+spaceID, map[string]any{"name": "No"}, 403)
	h.must(bob, "GET", "/api/v1/spaces/"+spaceID+"/grants", nil, 403)

	// Group grant on a single list: carol's group gets edit on one list and
	// sees nothing else in the space (TASK-1 cross-team sharing).
	sp2 := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Events", "key": "EVTX"}, 201)
	space2ID := str(sp2, "id")
	handoff := h.must(alice, "POST", "/api/v1/spaces/"+space2ID+"/lists", map[string]any{"name": "Handoffs"}, 201)
	handoffID := str(handoff, "id")
	otherListID := str(sp2["lists"].([]any)[0].(map[string]any), "id")
	h.must(alice, "PUT", "/api/v1/lists/"+handoffID+"/grants",
		map[string]any{"subject_type": "group", "subject": "sec-team", "level": "edit"}, 200)
	carolSec := as{user: "carol", groups: "sec-team"}
	h.must(carolSec, "GET", "/api/v1/lists/"+handoffID+"/tasks", nil, 200)
	h.must(carolSec, "POST", "/api/v1/tasks", map[string]any{"list_id": handoffID, "title": "from security"}, 201)
	h.must(carolSec, "GET", "/api/v1/lists/"+otherListID+"/tasks", nil, 404)
	spView := h.must(carolSec, "GET", "/api/v1/spaces/"+space2ID, nil, 200)
	if got := len(spView["lists"].([]any)); got != 1 {
		t.Fatalf("carol should see exactly the shared list, got %d lists", got)
	}
	if spView["shared_only"] != true {
		t.Fatalf("carol's view of the space should be shared_only")
	}
	// Without the group, carol sees nothing.
	h.must(carol, "GET", "/api/v1/lists/"+handoffID+"/tasks", nil, 404)

	// Private lists hide from space-level members (everyone_level=edit).
	h.must(alice, "PATCH", "/api/v1/spaces/"+space2ID, map[string]any{"everyone_level": "edit"}, 200)
	h.must(bob, "GET", "/api/v1/lists/"+otherListID+"/tasks", nil, 200)
	privList := h.must(alice, "POST", "/api/v1/spaces/"+space2ID+"/lists", map[string]any{"name": "Managers", "private": true}, 201)
	privID := str(privList, "id")
	h.must(bob, "GET", "/api/v1/lists/"+privID+"/tasks", nil, 404)
	h.must(alice, "GET", "/api/v1/lists/"+privID+"/tasks", nil, 200) // creator manages

	// Assignment opens that one task at edit level (closed space SEC).
	secTask := h.must(alice, "POST", "/api/v1/tasks",
		map[string]any{"list_id": listID, "title": "carol's job", "assignee_ids": []string{str(h.must(carol, "GET", "/api/v1/me", nil, 200), "id")}}, 201)
	secKey := str(secTask, "key")
	h.must(carol, "GET", "/api/v1/tasks/"+secKey, nil, 200)
	ver := secTask["version"].(float64)
	h.must(carol, "PATCH", "/api/v1/tasks/"+secKey, map[string]any{"priority": "high", "version": int(ver)}, 200)

	// Comment moderation (TASK-30): an editor cannot delete someone else's
	// comment; the space's creator (manage via creator rule) can.
	comment := h.must(bob, "POST", "/api/v1/tasks/"+taskKey+"/comments", map[string]any{"body": "bob was here"}, 201)
	commentID := str(comment, "id")
	h.must(carolSec, "DELETE", "/api/v1/comments/"+commentID, nil, 404) // carol can't even see the task
	comment2 := h.must(bob, "POST", "/api/v1/tasks/"+taskKey+"/comments", map[string]any{"body": "again"}, 201)
	h.must(alice, "DELETE", "/api/v1/comments/"+str(comment2, "id"), nil, 204)

	// TASK-33: emails only for callers who manage spaces.
	users := h.must(bob, "GET", "/api/v1/users", nil, 200)["users"].([]any)
	for _, raw := range users {
		if e, ok := raw.(map[string]any)["email"].(string); ok && e != "" {
			t.Fatalf("bob should not see emails, got %q", e)
		}
	}
	usersAdmin := h.must(admin, "GET", "/api/v1/users", nil, 200)["users"].([]any)
	sawEmail := false
	for _, raw := range usersAdmin {
		if e, ok := raw.(map[string]any)["email"].(string); ok && e != "" {
			sawEmail = true
		}
	}
	if !sawEmail {
		t.Fatal("admin should see emails in /users")
	}

	// TASK-29 + TASK-48: a delete trashes the task (it vanishes from views
	// but can be restored) and writes an audit row; a purge removes it for
	// good and the task events survive with their snapshot.
	h.must(alice, "DELETE", "/api/v1/tasks/"+secKey, nil, 204)
	h.must(alice, "GET", "/api/v1/tasks/"+secKey, nil, 404)
	var audits int
	pool := mustPool(t)
	defer pool.Close()
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity = 'task'`).Scan(&audits); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audits == 0 {
		t.Fatal("task delete should write an audit_log row")
	}
	restored := h.must(alice, "POST", "/api/v1/tasks/"+secKey+"/restore", nil, 200)
	if restored["key"] != secKey {
		t.Fatalf("restore returned %v, want %s", restored["key"], secKey)
	}
	h.must(alice, "DELETE", "/api/v1/tasks/"+secKey, nil, 204)
	h.must(alice, "DELETE", "/api/v1/tasks/"+secKey+"/purge", nil, 204)
	var orphanEvents int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM task_events WHERE task_id IS NULL AND task_key IS NOT NULL`).Scan(&orphanEvents); err != nil {
		t.Fatalf("events query: %v", err)
	}
	if orphanEvents == 0 {
		t.Fatal("task events should survive the purge with their snapshot")
	}
}

func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	return pool
}

// Guard against the dev-mode default ever being reachable outside
// development (TASK-34).
func TestDevModeRefusedOutsideDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_MODE", "dev")
	t.Setenv("DATABASE_URL", "postgres://x:x@localhost/x")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "AUTH_MODE=dev is only allowed") {
		t.Fatalf("want fail-closed config error, got %v", err)
	}
}
