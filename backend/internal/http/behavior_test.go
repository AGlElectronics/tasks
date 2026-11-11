package http

import (
	"testing"
)

// Behavioral tests (TASK-52/53/54) against a real Postgres, same harness
// and TEST_DATABASE_URL switch as TestAuthorization.

// TestUpdateTaskPatchSemantics (TASK-52): absent fields stay, null clears
// nullable fields, stale versions conflict.
func TestUpdateTaskPatchSemantics(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Patch", "key": "PAT"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")

	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "patch me", "priority": "high", "due_at": "2027-01-15", "description": "keep me",
	}, 201)
	id := str(task, "id")
	v := int(task["version"].(float64))

	// Patch only the title: everything else must survive.
	out := h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"title": "renamed", "version": v}, 200)
	if out["priority"] != "high" || out["due_at"] != "2027-01-15" || out["description"] != "keep me" {
		t.Fatalf("absent fields should be untouched, got priority=%v due=%v desc=%v", out["priority"], out["due_at"], out["description"])
	}
	v = int(out["version"].(float64))

	// Explicit nulls clear nullable fields.
	out = h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"priority": nil, "due_at": nil, "version": v}, 200)
	if out["priority"] != nil || out["due_at"] != nil {
		t.Fatalf("null should clear, got priority=%v due=%v", out["priority"], out["due_at"])
	}
	newV := int(out["version"].(float64))
	if newV <= v {
		t.Fatalf("version should bump on change, got %d after %d", newV, v)
	}

	// A stale version conflicts and changes nothing.
	rec, _ := h.do(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"title": "lost update", "version": v})
	if rec.Code != 409 {
		t.Fatalf("stale version: got %d want 409", rec.Code)
	}
	after := h.must(alice, "GET", "/api/v1/tasks/"+str(out, "key"), nil, 200)
	if after["title"] != "renamed" {
		t.Fatalf("conflicting patch must not apply, title=%v", after["title"])
	}

	// Missing version is a validation error; a foreign status is rejected.
	h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"title": "x"}, 422)
	other := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Other", "key": "OTH"}, 201)
	foreignStatus := str(other["statuses"].([]any)[0].(map[string]any), "id")
	h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"status_id": foreignStatus, "version": newV}, 422)
}

// TestReplaceStatuses (TASK-53): removed statuses remap tasks within the
// same category (cross-category as fallback) and completed_at follows the
// category of the task's status.
func TestReplaceStatuses(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Statuses", "key": "STS"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	statuses := sp["statuses"].([]any)
	type st struct{ id, name, category, color string }
	var all []st
	for _, raw := range statuses {
		m := raw.(map[string]any)
		all = append(all, st{str(m, "id"), str(m, "name"), str(m, "category"), str(m, "color")})
	}
	byCat := func(c string) st {
		for _, x := range all {
			if x.category == c {
				return x
			}
		}
		t.Fatalf("no default status with category %s", c)
		return st{}
	}
	active := byCat("active")
	notStarted := byCat("not_started")
	done := byCat("done")

	task := h.must(alice, "POST", "/api/v1/tasks",
		map[string]any{"list_id": listID, "title": "remap me", "status_id": active.id}, 201)

	// Replace the active status with a brand new active one: the task must
	// land on the same-category replacement.
	put := []map[string]any{
		{"id": notStarted.id, "name": notStarted.name, "color": notStarted.color, "category": "not_started"},
		{"name": "Doing 2", "color": "#1166ff", "category": "active"},
		{"id": done.id, "name": done.name, "color": done.color, "category": "done"},
	}
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": put}, 200)
	got := h.must(alice, "GET", "/api/v1/tasks/"+str(task, "key"), nil, 200)
	status := got["status"].(map[string]any)
	if status["name"] != "Doing 2" || status["category"] != "active" {
		t.Fatalf("task should remap to the same category, got %v", status)
	}
	if got["completed_at"] != nil {
		t.Fatal("active task must not be completed")
	}

	// Remove every open status except not_started: cross-category fallback.
	put = []map[string]any{
		{"id": notStarted.id, "name": notStarted.name, "color": notStarted.color, "category": "not_started"},
		{"id": done.id, "name": done.name, "color": done.color, "category": "done"},
	}
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": put}, 200)
	got = h.must(alice, "GET", "/api/v1/tasks/"+str(task, "key"), nil, 200)
	if got["status"].(map[string]any)["category"] == "active" {
		t.Fatal("the active status should be gone")
	}

	// Flip the task's current status to category done by editing it in
	// place: completed_at must appear; flipping back clears it.
	curID := str(got["status"].(map[string]any), "id")
	curName := str(got["status"].(map[string]any), "name")
	put = []map[string]any{
		{"id": curID, "name": curName, "color": "#444444", "category": "done"},
		{"id": done.id, "name": done.name, "color": done.color, "category": "not_started"},
	}
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": put}, 200)
	got = h.must(alice, "GET", "/api/v1/tasks/"+str(task, "key"), nil, 200)
	if got["completed_at"] == nil {
		t.Fatal("completed_at should be set when the status's category becomes done")
	}
	put = []map[string]any{
		{"id": curID, "name": curName, "color": "#444444", "category": "active"},
		{"id": done.id, "name": done.name, "color": done.color, "category": "done"},
	}
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": put}, 200)
	got = h.must(alice, "GET", "/api/v1/tasks/"+str(task, "key"), nil, 200)
	if got["completed_at"] != nil {
		t.Fatal("completed_at should clear when the category stops being done")
	}
	kinds := map[string]bool{}
	for _, raw := range got["events"].([]any) {
		kinds[str(raw.(map[string]any), "kind")] = true
	}
	if !kinds["completed"] || !kinds["reopened"] {
		t.Fatalf("category flips must be logged as completed and reopened, got %v", kinds)
	}

	// Guardrails: no statuses at all, and no done status, are both invalid.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": []any{}}, 422)
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/statuses", map[string]any{"statuses": []map[string]any{
		{"name": "Only open", "color": "#112233", "category": "active"},
	}}, 422)
}

// TestTaskKeyResolution (TASK-54): KEY-N references resolve case-
// insensitively, by uuid, and keep resolving after a space key rename and
// a cross-space move (TASK-45).
func TestTaskKeyResolution(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Resolve", "key": "RES"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "find me"}, 201)
	if str(task, "key") != "RES-1" {
		t.Fatalf("first task should be RES-1, got %s", str(task, "key"))
	}

	// By key, case-insensitively, and by uuid.
	h.must(alice, "GET", "/api/v1/tasks/RES-1", nil, 200)
	h.must(alice, "GET", "/api/v1/tasks/res-1", nil, 200)
	h.must(alice, "GET", "/api/v1/tasks/"+str(task, "id"), nil, 200)
	// Unknown and garbage references are 404.
	h.must(alice, "GET", "/api/v1/tasks/RES-999", nil, 404)
	h.must(alice, "GET", "/api/v1/tasks/not-a-ref", nil, 404)

	// Space key rename: both the new and the old key resolve (TASK-45).
	h.must(alice, "PATCH", "/api/v1/spaces/"+spaceID, map[string]any{"key": "REZ"}, 200)
	if got := str(h.must(alice, "GET", "/api/v1/tasks/REZ-1", nil, 200), "key"); got != "REZ-1" {
		t.Fatalf("renamed key should resolve, got %s", got)
	}
	if got := str(h.must(alice, "GET", "/api/v1/tasks/RES-1", nil, 200), "key"); got != "REZ-1" {
		t.Fatalf("old key should alias to the renamed task, got %s", got)
	}

	// Cross-space move: the task gets a new number in the target space and
	// every earlier reference still resolves.
	sp2 := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Target", "key": "TGT"}, 201)
	list2 := str(sp2["lists"].([]any)[0].(map[string]any), "id")
	// Burn a number in the target space so the move cannot keep "1".
	h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": list2, "title": "occupant"}, 201)
	cur := h.must(alice, "GET", "/api/v1/tasks/REZ-1", nil, 200)
	moved := h.must(alice, "PATCH", "/api/v1/tasks/"+str(cur, "id"),
		map[string]any{"list_id": list2, "version": int(cur["version"].(float64))}, 200)
	if str(moved, "key") != "TGT-2" {
		t.Fatalf("moved task should be TGT-2, got %s", str(moved, "key"))
	}
	for _, ref := range []string{"TGT-2", "REZ-1", "RES-1"} {
		if got := str(h.must(alice, "GET", "/api/v1/tasks/"+ref, nil, 200), "key"); got != "TGT-2" {
			t.Fatalf("%s should resolve to the moved task, got %s", ref, got)
		}
	}
}
