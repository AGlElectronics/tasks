package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AGlElectronics/tasks/backend/internal/config"
)

// hook sends HR's department hook request exactly as hr/backend's
// DepartmentService.notifyHooks does: POST, JSON body, bearer token.
func (h *harness) hook(token, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest("POST", "/api/internal/departments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestDepartmentHook(t *testing.T) {
	h := newHarnessWith(t, func(c *config.Config) { c.TasksServiceToken = "tasks-service-test-token" })
	const tok = "tasks-service-test-token"
	const dept = "5b0c7f1e-9d2a-4c3b-8e6f-1a2b3c4d5e6f"
	member := as{user: "maya", groups: "/Dept-Front-Of-House"}
	admin := as{user: "admin", groups: "tasks-admin"}

	// Create: HR's payload on department create.
	rec := h.hook(tok, `{"id":"`+dept+`","slug":"front-of-house","name":"Front of House","group":"dept-front-of-house"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	sp := h.must(member, "GET", "/api/v1/spaces", nil, 200)["spaces"].([]any)
	if len(sp) != 1 {
		t.Fatalf("the department group should see its space, got %d spaces", len(sp))
	}
	space := sp[0].(map[string]any)
	if space["key"] != "FOH" || space["my_level"] != "edit" || space["department_id"] != dept || space["archived"] != false {
		t.Fatalf("department space: %v", space)
	}
	spaceID := str(space, "id")
	if n := len(space["lists"].([]any)); n != 3 {
		t.Fatalf("want the 3 default department lists, got %d", n)
	}
	h.must(as{user: "outsider", groups: "dept-kitchen"}, "GET", "/api/v1/spaces/"+spaceID, nil, 404)

	// Update (rename) on department save: same space, 200. HR may add
	// fields; unknown ones are ignored.
	rec = h.hook(tok, `{"id":"`+dept+`","slug":"front-of-house","name":"FOH & Bar","group":"dept-front-of-house","description":"new field"}`)
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got := h.must(member, "GET", "/api/v1/spaces/"+spaceID, nil, 200)
	if got["name"] != "FOH & Bar" {
		t.Fatalf("rename should follow HR, got %v", got["name"])
	}

	// Archive: the space leaves the sidebar but keeps its content.
	rec = h.hook(tok, `{"id":"`+dept+`","slug":"front-of-house","name":"FOH & Bar","group":"dept-front-of-house","archived":true}`)
	if rec.Code != 200 {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(h.must(member, "GET", "/api/v1/spaces", nil, 200)["spaces"].([]any)); n != 0 {
		t.Fatalf("archived space should leave the sidebar, got %d", n)
	}
	all := h.must(member, "GET", "/api/v1/spaces?archived=1", nil, 200)["spaces"].([]any)
	if len(all) != 1 || all[0].(map[string]any)["archived"] != true {
		t.Fatalf("?archived=1 should list it as archived, got %v", all)
	}
	h.must(member, "GET", "/api/v1/spaces/"+spaceID, nil, 200)

	// A save without "archived" means the department is active again.
	rec = h.hook(tok, `{"id":"`+dept+`","slug":"front-of-house","name":"FOH & Bar","group":"dept-front-of-house"}`)
	if rec.Code != 200 {
		t.Fatalf("restore: %d", rec.Code)
	}
	if n := len(h.must(member, "GET", "/api/v1/spaces", nil, 200)["spaces"].([]any)); n != 1 {
		t.Fatalf("restored space should be back, got %d", n)
	}

	// The archive and restore are in the audit log, by the system.
	var n int
	if err := h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM audit_log WHERE entity = 'space' AND entity_id = $1 AND actor_id IS NULL
		   AND action IN ('archived', 'restored')`, spaceID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("want archived+restored audit rows, got %d (%v)", n, err)
	}

	// A manager can archive by hand too.
	h.must(admin, "PATCH", "/api/v1/spaces/"+spaceID, map[string]any{"archived": true}, 200)
	h.must(admin, "PATCH", "/api/v1/spaces/"+spaceID, map[string]any{"archived": false}, 200)

	// Errors HR can see.
	if rec := h.hook("", `{}`); rec.Code != 401 {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := h.hook("wrong", `{}`); rec.Code != 401 {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	if rec := h.hook(tok, `{"id":"`+dept+`"}`); rec.Code != 422 {
		t.Fatalf("missing fields: %d %s", rec.Code, rec.Body.String())
	}
	if rec := h.hook(tok, `not json`); rec.Code != 400 {
		t.Fatalf("bad JSON: %d", rec.Code)
	}

	// Another department with a colliding key gets a numbered one.
	rec = h.hook(tok, `{"id":"6b0c7f1e-9d2a-4c3b-8e6f-1a2b3c4d5e6f","slug":"floor-of-hall","name":"Floor of Hall","group":"dept-floor-of-hall"}`)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"key":"FOH2"`) {
		t.Fatalf("colliding key: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDepartmentHookOffWithoutToken(t *testing.T) {
	h := newHarness(t)
	if rec := h.hook("anything", `{}`); rec.Code != 404 {
		t.Fatalf("without TASKS_SERVICE_TOKEN the route must 404, got %d", rec.Code)
	}
}
