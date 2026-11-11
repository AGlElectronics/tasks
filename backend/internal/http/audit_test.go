package http

import (
	"testing"
)

// TestAuditLogAPI: admins read the audit log newest first, filter it and
// page through it; everyone else gets 403.
func TestAuditLogAPI(t *testing.T) {
	h := newHarness(t)
	admin := as{user: "admin", groups: "tasks-admin"}
	alice := as{user: "alice", groups: "tasks-creator"}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Audit", "key": "AUD"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	for i := 0; i < 3; i++ {
		task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "gone"}, 201)
		h.must(alice, "DELETE", "/api/v1/tasks/"+str(task, "id"), nil, 204)
	}
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants", map[string]any{"subject_type": "group", "subject": "crew", "level": "view"}, 200)

	h.must(alice, "GET", "/api/v1/audit", nil, 403)
	h.must(as{user: "bob"}, "GET", "/api/v1/audit", nil, 403)

	all := h.must(admin, "GET", "/api/v1/audit", nil, 200)["entries"].([]any)
	if len(all) != 4 {
		t.Fatalf("want 4 entries, got %d", len(all))
	}
	first := all[0].(map[string]any)
	if first["entity"] != "grant" || first["action"] != "granted" || first["actor_name"] != "Alice" {
		t.Fatalf("newest first: %v", first)
	}

	tasksOnly := h.must(admin, "GET", "/api/v1/audit?entity=task&action=deleted", nil, 200)["entries"].([]any)
	if len(tasksOnly) != 3 || str(tasksOnly[0].(map[string]any)["data"].(map[string]any), "title") != "gone" {
		t.Fatalf("entity filter: %v", tasksOnly)
	}

	// Paging: two pages of 2.
	rec, page := h.do(admin, "GET", "/api/v1/audit?limit=2", nil)
	next := rec.Header().Get("X-Next-Cursor")
	if len(page["entries"].([]any)) != 2 || next == "" {
		t.Fatalf("page 1: %v next=%q", page, next)
	}
	rec, page = h.do(admin, "GET", "/api/v1/audit?limit=2&cursor="+next, nil)
	if len(page["entries"].([]any)) != 2 || rec.Header().Get("X-Next-Cursor") != "" {
		t.Fatalf("page 2 should be the last: %v", page)
	}

	if n := len(h.must(admin, "GET", "/api/v1/audit?since=2999-01-01", nil, 200)["entries"].([]any)); n != 0 {
		t.Fatalf("since in the future: %d", n)
	}
	h.must(admin, "GET", "/api/v1/audit?entity=nope", nil, 422)
	h.must(admin, "GET", "/api/v1/audit?actor=nope", nil, 422)
	h.must(admin, "GET", "/api/v1/audit?since=yesterday", nil, 422)
}
