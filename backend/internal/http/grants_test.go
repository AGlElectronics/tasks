package http

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/AGlElectronics/tasks/backend/internal/config"
	"github.com/AGlElectronics/tasks/backend/internal/groupmatch"
	"github.com/AGlElectronics/tasks/backend/migrations"
)

// TestGroupGrantsMatchNormalised: group grants match the shared rule —
// case-insensitive, one leading '/' ignored, whole path compared — and the
// UI keeps showing the name as typed.
func TestGroupGrantsMatchNormalised(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Security", "key": "SEC"}, 201)
	spaceID := str(sp, "id")
	g := h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "group", "subject": " /Dept-Security ", "level": "view"}, 200)
	if str(g, "subject") != "/Dept-Security" || str(g, "subject_key") != "dept-security" || str(g, "display_name") != "/Dept-Security" {
		t.Fatalf("grant should keep the typed name and store the key, got %v", g)
	}

	for _, groups := range []string{"dept-security", "/dept-security", "DEPT-SECURITY", "x, /Dept-Security"} {
		h.must(as{user: "member", groups: groups}, "GET", "/api/v1/spaces/"+spaceID, nil, 200)
	}
	// Full-path match only: neither a sub-group nor a parent's last segment.
	for _, groups := range []string{"security", "dept-security/team", "/org/dept-security", ""} {
		h.must(as{user: "outsider", groups: groups}, "GET", "/api/v1/spaces/"+spaceID, nil, 404)
	}

	// The same group spelled differently updates the one grant.
	again := h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "group", "subject": "DEPT-SECURITY", "level": "edit"}, 200)
	if str(again, "id") != str(g, "id") || str(again, "level") != "edit" || str(again, "subject") != "DEPT-SECURITY" {
		t.Fatalf("re-grant should update the existing row, got %v", again)
	}
	list := h.must(alice, "GET", "/api/v1/spaces/"+spaceID+"/grants", nil, 200)
	if n := len(list["grants"].([]any)); n != 1 {
		t.Fatalf("want one grant, got %d", n)
	}

	// Full paths work as long as both sides name the same path.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "group", "subject": "/Departments/Ops", "level": "view"}, 200)
	h.must(as{user: "ops", groups: "departments/ops"}, "GET", "/api/v1/spaces/"+spaceID, nil, 200)
	h.must(as{user: "ops2", groups: "ops"}, "GET", "/api/v1/spaces/"+spaceID, nil, 404)

	// "/" alone normalises to nothing and is rejected.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "group", "subject": " / ", "level": "view"}, 422)

	// Grant changes land in the audit log.
	var n int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity = 'grant' AND action = 'granted'`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("want 3 granted audit rows, got %d (%v)", n, err)
	}
	h.must(alice, "DELETE", "/api/v1/spaces/"+spaceID+"/grants/"+str(g, "id"), nil, 204)
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity = 'grant' AND action = 'revoked' AND data->>'subject' = 'DEPT-SECURITY'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("want a revoked audit row, got %d (%v)", n, err)
	}
	h.must(as{user: "member", groups: "dept-security"}, "GET", "/api/v1/spaces/"+spaceID, nil, 404)
}

// TestGroupGrantsPrefixAndLast: OIDC_GROUP_PREFIX and OIDC_GROUP_MATCH=last
// apply to grants and to access-config bindings alike.
func TestGroupGrantsPrefixAndLast(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) {
		c.Groups = groupmatch.Config{Mode: groupmatch.ModeLast, Prefix: "/Con2026"}
	})
	alice := as{user: "alice", groups: "/con2026/tasks-creator"}
	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Security", "key": "SEC"}, 201)
	spaceID := str(sp, "id")
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "group", "subject": "/Departments/Security", "level": "view"}, 200)

	for _, groups := range []string{"/con2026/departments/security", "/teams/Security", "security"} {
		h.must(as{user: "member", groups: groups}, "GET", "/api/v1/spaces/"+spaceID, nil, 200)
	}
	for _, groups := range []string{"/con2026", "/departments/security-lead", ""} {
		h.must(as{user: "outsider", groups: groups}, "GET", "/api/v1/spaces/"+spaceID, nil, 404)
	}
	sidebar := h.must(as{user: "member", groups: "/teams/security"}, "GET", "/api/v1/spaces", nil, 200)
	if n := len(sidebar["spaces"].([]any)); n != 1 {
		t.Fatalf("the sidebar should list the granted space, got %d", n)
	}
}

// TestGroupKeySQLMatchesGo: tasks_group_key on a stored subject_key gives
// what groupmatch.Config.Key gives for the name the admin typed.
func TestGroupKeySQLMatchesGo(t *testing.T) {
	h := newHarness(t)
	names := []string{"den-admin", " /Departments/Security ", "/con2026/den-admin", "/con2026", "con2026/x/",
		"/con2025/den-admin", "departments/security/", "//double", "/CON2026/Teams/Bar", "a/b/c"}
	for _, tc := range []struct {
		c            groupmatch.Config
		prefix, mode string // as groupMatchArgs passes them (tested in package tasks)
	}{
		{groupmatch.Config{}, "", "full"},
		{groupmatch.Config{Mode: groupmatch.ModeLast}, "", "last"},
		{groupmatch.Config{Prefix: "/con2026"}, "con2026", "full"},
		{groupmatch.Config{Prefix: "con2026/", Mode: groupmatch.ModeLast}, "con2026", "last"},
	} {
		for _, n := range names {
			var got string
			if err := h.pool.QueryRow(context.Background(), `SELECT tasks_group_key($1, $2, $3)`,
				groupmatch.NewGrant(n).Stored, tc.prefix, tc.mode).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := tc.c.Key(n); got != want {
				t.Errorf("%+v %q: SQL %q, Go %q", tc.c, n, got, want)
			}
		}
	}
}

// TestGrantSubjectKeyBackfill runs the migration over grants written before
// it: keys are backfilled, and grants that now name the same group collapse
// into the strongest one.
func TestGrantSubjectKeyBackfill(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 20261106000004); err != nil {
		t.Fatalf("migrate to the version before: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO spaces (id, organization_id, name, key)
		VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000010', 'Old', 'OLD');
		INSERT INTO access_grants (space_id, subject_type, subject, level, created_at) VALUES
		  ('00000000-0000-0000-0000-0000000000a1', 'group', 'Dept-Kitchen', 'view', now() - interval '3 days'),
		  ('00000000-0000-0000-0000-0000000000a1', 'group', '/dept-kitchen', 'manage', now() - interval '2 days'),
		  ('00000000-0000-0000-0000-0000000000a1', 'group', 'DEPT-KITCHEN', 'manage', now() - interval '1 day'),
		  ('00000000-0000-0000-0000-0000000000a1', 'group', ' /Departments/Bar ', 'edit', now()),
		  ('00000000-0000-0000-0000-0000000000a1', 'user', '11111111-1111-1111-1111-111111111111', 'edit', now())`); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT subject, subject_key, level FROM access_grants ORDER BY subject_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type grant struct{ subject, key, level string }
	var got []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.subject, &g.key, &g.level); err != nil {
			t.Fatal(err)
		}
		got = append(got, g)
	}
	want := []grant{
		{"11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111", "edit"},
		{" /Departments/Bar ", "departments/bar", "edit"},
		{"/dept-kitchen", "dept-kitchen", "manage"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}
