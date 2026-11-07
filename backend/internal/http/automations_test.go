package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// Automation tests (TASK-22) against a real Postgres, same harness and
// TEST_DATABASE_URL switch as TestAuthorization.

// statusByCategory digs a status id out of a space payload.
func statusByCategory(sp map[string]any, category string) string {
	for _, raw := range sp["statuses"].([]any) {
		st := raw.(map[string]any)
		if st["category"] == category {
			return str(st, "id")
		}
	}
	return ""
}

func TestAutomations(t *testing.T) {
	h := newHarness(t)
	admin := as{user: "admin", groups: "tasks-admin"}
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob", groups: ""}

	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Auto", "key": "AUTO"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	notStarted := statusByCategory(sp, "not_started")
	activeSt := statusByCategory(sp, "active")
	doneSt := statusByCategory(sp, "done")
	if notStarted == "" || activeSt == "" || doneSt == "" {
		t.Fatalf("expected default statuses of all three categories, got %v", sp["statuses"])
	}

	// --- Validation ------------------------------------------------------
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "bogus",
		"trigger": map[string]any{"type": "wat"},
		"actions": []any{map[string]any{"type": "set_priority", "priority": "high"}},
	}, 422)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "no actions",
		"trigger": map[string]any{"type": "task_created"},
		"actions": []any{},
	}, 422)

	// --- Permissions ------------------------------------------------------
	// Bob has only edit on the space: no automation rights.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants",
		map[string]any{"subject_type": "user", "subject": bobID, "level": "edit"}, 200)
	h.must(bob, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "nope",
		"trigger": map[string]any{"type": "task_created"},
		"actions": []any{map[string]any{"type": "set_priority", "priority": "high"}},
	}, 403)
	// With manage on one list, bob can create a rule scoped to that list…
	h.must(alice, "PUT", "/api/v1/lists/"+listID+"/grants",
		map[string]any{"subject_type": "user", "subject": bobID, "level": "manage"}, 200)
	scoped := h.must(bob, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "bob's list rule",
		"list_id": listID,
		"trigger": map[string]any{"type": "comment_added"},
		"actions": []any{map[string]any{"type": "set_priority", "priority": "low"}},
	}, 201)
	// …but not a space-wide one.
	h.must(bob, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "space-wide",
		"trigger": map[string]any{"type": "task_created"},
		"actions": []any{map[string]any{"type": "set_priority", "priority": "high"}},
	}, 403)
	// Bob's listing shows only his list's rules.
	bobList := h.must(bob, "GET", "/api/v1/spaces/"+spaceID+"/automations", nil, 200)
	if n := len(bobList["automations"].([]any)); n != 1 {
		t.Fatalf("bob should see exactly his list-scoped rule, got %d", n)
	}
	h.must(bob, "DELETE", "/api/v1/automations/"+str(scoped, "id"), nil, 204)

	// --- task_created: set priority + add a checklist item ---------------
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "triage new tasks",
		"trigger": map[string]any{"type": "task_created"},
		"actions": []any{
			map[string]any{"type": "set_priority", "priority": "high"},
			map[string]any{"type": "add_checklist_item", "body": "estimate it"},
		},
	}, 201)
	created := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "fresh"}, 201)
	detail := h.must(alice, "GET", "/api/v1/tasks/"+str(created, "key"), nil, 200)
	if detail["priority"] != "high" {
		t.Fatalf("task_created rule should have set priority high, got %v", detail["priority"])
	}
	if n := len(detail["checklist"].([]any)); n != 1 {
		t.Fatalf("task_created rule should have added one checklist item, got %d", n)
	}

	// --- status_changed → done: comment + assign; chained assignee rule --
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "done housekeeping",
		"trigger": map[string]any{"type": "status_changed", "status_id": doneSt},
		"actions": []any{
			map[string]any{"type": "add_comment", "body": "closed, thanks!"},
			map[string]any{"type": "assign", "user_id": bobID},
		},
	}, 201)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "new assignee gets a due date",
		"trigger": map[string]any{"type": "assignee_added"},
		"actions": []any{map[string]any{"type": "set_due", "days": 3}},
	}, 201)

	version := int(detail["version"].(float64))
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(created, "id"),
		map[string]any{"status_id": doneSt, "version": version}, 200)
	detail = h.must(alice, "GET", "/api/v1/tasks/"+str(created, "key"), nil, 200)
	comments := detail["comments"].([]any)
	foundAuto := false
	for _, raw := range comments {
		c := raw.(map[string]any)
		if c["body"] == "closed, thanks!" {
			foundAuto = true
			if !strings.Contains(str(c, "author_name"), "done housekeeping") {
				t.Fatalf("automation comment should carry the rule name, got author %q", str(c, "author_name"))
			}
		}
	}
	if !foundAuto {
		t.Fatal("status rule should have commented")
	}
	if n := len(detail["assignees"].([]any)); n != 1 {
		t.Fatalf("status rule should have assigned bob, got %d assignees", n)
	}
	if detail["due_at"] == nil {
		t.Fatal("chained assignee rule should have set a due date")
	}
	want := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	if detail["due_at"] != want {
		t.Fatalf("due date: got %v want %s", detail["due_at"], want)
	}

	// --- Loop guard: two rules that flip status back and forth ------------
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "ping",
		"trigger": map[string]any{"type": "status_changed", "status_id": activeSt},
		"actions": []any{map[string]any{"type": "set_status", "status_id": doneSt}},
	}, 201)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "pong",
		"trigger": map[string]any{"type": "status_changed", "status_id": doneSt},
		"actions": []any{map[string]any{"type": "set_status", "status_id": activeSt}},
	}, 201)
	loopTask := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "loopy"}, 201)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(loopTask, "id"),
		map[string]any{"status_id": activeSt, "version": int(loopTask["version"].(float64))}, 200)
	// ping fired (→ done), pong fired once (→ active), ping already visited:
	// the chain must stop with the task on a stable status, not time out.
	after := h.must(alice, "GET", "/api/v1/tasks/"+str(loopTask, "key"), nil, 200)
	if cat := after["status"].(map[string]any)["id"]; cat != activeSt {
		t.Fatalf("loop should settle on active after ping+pong each fired once, got status %v", cat)
	}

	// Run history exists and is readable by a manager.
	autos := h.must(admin, "GET", "/api/v1/spaces/"+spaceID+"/automations", nil, 200)["automations"].([]any)
	var pingID string
	for _, raw := range autos {
		a := raw.(map[string]any)
		if a["name"] == "ping" {
			pingID = str(a, "id")
		}
	}
	runs := h.must(admin, "GET", "/api/v1/automations/"+pingID+"/runs", nil, 200)["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("ping should have exactly one run, got %d", len(runs))
	}

	// Disable pong; a fresh flip to done must stay done.
	var pongID string
	for _, raw := range autos {
		a := raw.(map[string]any)
		if a["name"] == "pong" {
			pongID = str(a, "id")
		}
	}
	h.must(alice, "PATCH", "/api/v1/automations/"+pongID, map[string]any{"enabled": false}, 200)
	// Also disable ping so it stops interfering with the sweep test below.
	h.must(alice, "PATCH", "/api/v1/automations/"+pingID, map[string]any{"enabled": false}, 200)

	// --- due_approaching sweep --------------------------------------------
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/automations", map[string]any{
		"name":    "due soon → urgent",
		"trigger": map[string]any{"type": "due_approaching", "days_before": 1},
		"actions": []any{map[string]any{"type": "set_priority", "priority": "urgent"}},
	}, 201)
	today := time.Now().Format("2006-01-02")
	dueTask := h.must(alice, "POST", "/api/v1/tasks",
		map[string]any{"list_id": listID, "title": "deadline", "due_at": today}, 201)

	svc := tasks.NewService(h.pool)
	if _, err := svc.RunDueSweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	swept := h.must(alice, "GET", "/api/v1/tasks/"+str(dueTask, "key"), nil, 200)
	if swept["priority"] != "urgent" {
		t.Fatalf("sweep should have set priority urgent, got %v", swept["priority"])
	}
	// A second sweep must not re-fire for the same pair.
	if n, err := svc.RunDueSweep(context.Background()); err != nil || n != 0 {
		t.Fatalf("second sweep should be a no-op, got n=%d err=%v", n, err)
	}
}
