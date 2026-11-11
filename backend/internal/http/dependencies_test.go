package http

import (
	"testing"
)

func TestDependencies(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Deps", "key": "DEP"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	done := statusByCategory(sp, "done")
	mk := func(title string) map[string]any {
		return h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": title}, 201)
	}
	a, b, c := mk("Book venue"), mk("Print tickets"), mk("Sell tickets")

	// c waits for b, b waits for a.
	ref := h.must(alice, "POST", "/api/v1/tasks/"+str(c, "key")+"/dependencies", map[string]any{"depends_on": str(b, "key")}, 201)
	if ref["key"] != "DEP-2" {
		t.Fatalf("ref: %v", ref)
	}
	h.must(alice, "POST", "/api/v1/tasks/"+str(b, "id")+"/dependencies", map[string]any{"depends_on": str(a, "id")}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+str(b, "id")+"/dependencies", map[string]any{"depends_on": str(a, "id")}, 201) // idempotent

	// Cycles and self-references are refused.
	h.must(alice, "POST", "/api/v1/tasks/"+str(a, "id")+"/dependencies", map[string]any{"depends_on": str(c, "id")}, 422)
	h.must(alice, "POST", "/api/v1/tasks/"+str(a, "id")+"/dependencies", map[string]any{"depends_on": str(a, "id")}, 422)
	h.must(alice, "POST", "/api/v1/tasks/"+str(a, "id")+"/dependencies", map[string]any{"depends_on": "DEP-99"}, 404)

	d := h.must(alice, "GET", "/api/v1/tasks/"+str(b, "id"), nil, 200)
	if len(d["blocked_by"].([]any)) != 1 || len(d["blocking"].([]any)) != 1 || d["blocked_by_open"] != float64(1) {
		t.Fatalf("b: blocked_by=%v blocking=%v open=%v", d["blocked_by"], d["blocking"], d["blocked_by_open"])
	}

	// Finishing a unblocks b.
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(a, "id"), map[string]any{"status_id": done, "version": a["version"]}, 200)
	if got := h.must(alice, "GET", "/api/v1/tasks/"+str(b, "id"), nil, 200); got["blocked_by_open"] != float64(0) {
		t.Fatalf("b should be unblocked: %v", got["blocked_by_open"])
	}

	// Dependencies the caller cannot see are hidden, not leaked.
	other := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Secret", "key": "SCR"}, 201)
	secret := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": str(other["lists"].([]any)[0].(map[string]any), "id"), "title": "Hidden"}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+str(c, "id")+"/dependencies", map[string]any{"depends_on": str(secret, "id")}, 201)
	c = h.must(alice, "PATCH", "/api/v1/tasks/"+str(c, "id"), map[string]any{"assignee_ids": []string{bobID}, "version": c["version"]}, 200)
	asBob := h.must(bob, "GET", "/api/v1/tasks/"+str(c, "id"), nil, 200)
	// Neither blocker is in a space bob can see; c is open to him only
	// because he is assigned.
	if len(asBob["blocked_by"].([]any)) != 0 || asBob["hidden_dependencies"] != float64(2) || asBob["blocked_by_open"] != float64(2) {
		t.Fatalf("bob sees %v, hidden %v", asBob["blocked_by"], asBob["hidden_dependencies"])
	}
	// Bob may edit c (assigned) but cannot link a task he cannot see.
	h.must(bob, "POST", "/api/v1/tasks/"+str(c, "id")+"/dependencies", map[string]any{"depends_on": str(secret, "key")}, 404)

	h.must(alice, "DELETE", "/api/v1/tasks/"+str(c, "id")+"/dependencies/"+str(b, "key"), nil, 204)
	h.must(alice, "DELETE", "/api/v1/tasks/"+str(c, "id")+"/dependencies/"+str(b, "key"), nil, 404)
}

func TestRecurringTasks(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Repeat", "key": "REP"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	done := statusByCategory(sp, "done")
	tag := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/tags", map[string]any{"name": "ops"}, 201)

	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "Check fire extinguishers", "due_at": "2027-01-31", "start_at": "2027-01-30", "assignee_ids": []string{bobID},
	}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+str(task, "id")+"/checklist", map[string]any{"body": "Hall A"}, 201)
	task = h.must(alice, "GET", "/api/v1/tasks/"+str(task, "id"), nil, 200)
	task = h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{
		"version": task["version"], "tag_ids": []string{str(tag, "id")}, "recurrence": map[string]any{"freq": "monthly", "interval": 1},
	}, 200)
	if r := task["recurrence"].(map[string]any); r["freq"] != "monthly" || r["interval"] != float64(1) {
		t.Fatalf("recurrence: %v", task["recurrence"])
	}
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{"version": task["version"], "recurrence": map[string]any{"freq": "hourly"}}, 422)

	// Completing it creates the next one, a month later, with the rule.
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{"version": task["version"], "status_id": done}, 200)
	old := h.must(alice, "GET", "/api/v1/tasks/"+str(task, "id"), nil, 200)
	if old["recurrence"] != nil {
		t.Fatal("the rule moves to the next occurrence")
	}
	// PostgreSQL clamps Jan 31 + 1 month to Feb 28.
	next := h.must(alice, "GET", "/api/v1/tasks/REP-2", nil, 200)
	if next["due_at"] != "2027-02-28" || next["start_at"] != "2027-02-28" || next["status"].(map[string]any)["category"] != "not_started" ||
		next["recurrence"] == nil || len(next["assignees"].([]any)) != 1 || len(next["tags"].([]any)) != 1 ||
		next["checklist_total"] != float64(1) || next["checklist_done"] != float64(0) {
		t.Fatalf("next occurrence: %v", next)
	}
	kinds := map[string]bool{}
	for _, e := range old["events"].([]any) {
		kinds[str(e.(map[string]any), "kind")] = true
	}
	if !kinds["recurred"] || !kinds["recurrence"] {
		t.Fatalf("events: %v", kinds)
	}

	// Subtasks cannot repeat; completing an automation-driven task also recurs.
	sub := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"parent_id": str(next, "id"), "title": "sub"}, 201)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(sub, "id"), map[string]any{"version": sub["version"], "recurrence": map[string]any{"freq": "daily"}}, 422)

	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name": "close on comment", "trigger": map[string]any{"type": "comment_added"},
		"actions": []map[string]any{{"type": "set_status", "status_id": done}},
	}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+str(next, "id")+"/comments", map[string]any{"body": "done"}, 201)
	third := h.must(alice, "GET", "/api/v1/tasks/REP-4", nil, 200)
	if third["due_at"] != "2027-03-28" || third["title"] != "Check fire extinguishers" {
		t.Fatalf("third occurrence via automation: %v", third)
	}
}
