package http

import (
	"strings"
	"testing"
)

func TestImport(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Import", "key": "IMP"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	path := "/api/v1/lists/" + listID + "/import"

	csv := "Title,Status,Priority,Due Date,Assignees,Tags,Description\n" +
		"Book bus,In progress,high,2027-04-01,bob@tasks.local,\"transport, budget\",Two coaches\n" +
		"Pay deposit,Done,,05/04/2027,,budget,\n" +
		",To do,,,,,missing title\n" +
		"Call hotel,Waiting,whenever,next week,nobody@example.org,,\n"

	// Dry run: preview and warnings, nothing written.
	dry := h.must(alice, "POST", path, map[string]any{"format": "csv", "content": csv, "dry_run": true}, 200)
	if dry["created"] != float64(3) || dry["skipped"] != float64(1) || len(dry["preview"].([]any)) != 3 {
		t.Fatalf("dry run: %v", dry)
	}
	warnings := strings.Join(toStrings(dry["warnings"]), "\n")
	for _, want := range []string{"no title", `status "Waiting"`, `priority "whenever"`, `"next week"`, "nobody@example.org"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("missing warning %q in:\n%s", want, warnings)
		}
	}
	if n := len(h.must(alice, "GET", "/api/v1/lists/"+listID+"/tasks", nil, 200)["tasks"].([]any)); n != 0 {
		t.Fatalf("a dry run writes nothing, found %d tasks", n)
	}

	res := h.must(alice, "POST", path, map[string]any{"format": "csv", "content": csv}, 201)
	if res["created"] != float64(3) || len(res["new_tags"].([]any)) != 2 {
		t.Fatalf("import: %v", res)
	}
	got := h.must(alice, "GET", "/api/v1/tasks/IMP-1", nil, 200)
	if got["title"] != "Book bus" || got["priority"] != "high" || got["due_at"] != "2027-04-01" ||
		got["status"].(map[string]any)["name"] != "In progress" || len(got["assignees"].([]any)) != 1 ||
		len(got["tags"].([]any)) != 2 || got["description"] != "Two coaches" {
		t.Fatalf("imported task: %v", got)
	}
	paid := h.must(alice, "GET", "/api/v1/tasks/IMP-2", nil, 200)
	if paid["status"].(map[string]any)["category"] != "done" || paid["completed_at"] == nil || paid["due_at"] != "2027-04-05" {
		t.Fatalf("done task: %v", paid)
	}

	// ClickUp CSV: its column names, bracketed lists and Unix-ms dates.
	clickup := "Task ID,Task Name,Task Content,Status,Due Date,Assignees,Priority,Tags\n" +
		"86abc,Order radios,From the usual shop,complete,1804204800000,[bob@tasks.local],2,[transport]\n"
	res = h.must(alice, "POST", path, map[string]any{"format": "clickup", "content": clickup}, 201)
	if res["created"] != float64(1) {
		t.Fatalf("clickup: %v", res)
	}
	radios := h.must(alice, "GET", "/api/v1/tasks/IMP-4", nil, 200)
	if radios["due_at"] != "2027-03-05" || radios["status"].(map[string]any)["category"] != "done" || radios["priority"] != "high" {
		t.Fatalf("clickup task: %v", radios)
	}

	// Vikunja data.json: projects with tasks (and buckets), HTML descriptions.
	vikunja := `[{"title":"Crew","tasks":[{"title":"Briefing","description":"<p>Bring <b>badges</b></p>","done":false,
	  "due_date":"2027-05-01T18:00:00Z","start_date":"0001-01-01T00:00:00Z","priority":4,"labels":[{"title":"crew"}],
	  "assignees":[{"username":"bob","email":"bob@tasks.local"}]}],
	  "buckets":[{"tasks":[{"title":"Debrief","done":true,"priority":0}]}]}]`
	res = h.must(alice, "POST", path, map[string]any{"format": "vikunja", "content": vikunja}, 201)
	if res["created"] != float64(2) {
		t.Fatalf("vikunja: %v", res)
	}
	brief := h.must(alice, "GET", "/api/v1/tasks/IMP-5", nil, 200)
	if brief["description"] != "Bring badges" || brief["priority"] != "urgent" || brief["due_at"] != "2027-05-01" || brief["start_at"] != nil {
		t.Fatalf("vikunja task: %v", brief)
	}

	// Editors without manage cannot create tags; errors for bad input.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants", map[string]any{"subject_type": "user", "subject": bobID, "level": "edit"}, 200)
	res = h.must(bob, "POST", path, map[string]any{"format": "csv", "content": "Title,Tags\nX,brand-new\n"}, 201)
	if len(res["new_tags"].([]any)) != 0 || !strings.Contains(strings.Join(toStrings(res["warnings"]), " "), "only space managers") {
		t.Fatalf("tag creation by an editor: %v", res)
	}
	h.must(as{user: "carol"}, "POST", path, map[string]any{"format": "csv", "content": "Title\nX\n"}, 404)
	h.must(alice, "POST", path, map[string]any{"format": "trello", "content": "x"}, 422)
	h.must(alice, "POST", path, map[string]any{"format": "csv", "content": "Foo\nX\n"}, 422)
	h.must(alice, "POST", path, map[string]any{"format": "vikunja", "content": "{nope"}, 422)

	audit := h.must(as{user: "admin", groups: "tasks-admin"}, "GET", "/api/v1/audit?entity=list&action=imported", nil, 200)["entries"].([]any)
	if len(audit) < 4 {
		t.Fatalf("imports are audited: %d", len(audit))
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
