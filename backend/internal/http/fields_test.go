package http

import (
	"testing"
)

func TestCustomFields(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Fields", "key": "FLD"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants", map[string]any{"subject_type": "user", "subject": bobID, "level": "edit"}, 200)

	// Only managers define fields.
	h.must(bob, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "Cost", "type": "number"}, 403)
	cost := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "Cost", "type": "number"}, 201)
	vendor := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{
		"name": "Stage", "type": "select", "options": []map[string]any{{"name": "Ordered", "color": "#22c55e"}, {"name": "Delivered"}},
	}, 201)
	link := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "Quote", "type": "url"}, 201)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "cost", "type": "text"}, 409)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "Empty", "type": "select"}, 422)
	h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/fields", map[string]any{"name": "X", "type": "rating"}, 422)

	list := h.must(bob, "GET", "/api/v1/spaces/"+spaceID+"/fields", nil, 200)["fields"].([]any)
	if len(list) != 3 {
		t.Fatalf("fields: %v", list)
	}
	opts := vendor["options"].([]any)
	ordered := str(opts[0].(map[string]any), "id")
	delivered := str(opts[1].(map[string]any), "id")

	// Editors set values; each type is checked.
	task := h.must(bob, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "Stage lights"}, 201)
	id := str(task, "id")
	if len(task["fields"].(map[string]any)) != 0 {
		t.Fatalf("new task has no values: %v", task["fields"])
	}
	out := h.must(bob, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": task["version"], "fields": map[string]any{
		str(cost, "id"): 1250.5, str(vendor, "id"): ordered, str(link, "id"): "https://example.org/quote.pdf",
	}}, 200)
	f := out["fields"].(map[string]any)
	if f[str(cost, "id")] != 1250.5 || f[str(vendor, "id")] != ordered {
		t.Fatalf("values: %v", f)
	}
	for _, bad := range []map[string]any{
		{str(cost, "id"): "lots"},
		{str(vendor, "id"): "00000000-0000-0000-0000-000000000000"},
		{str(link, "id"): "javascript:alert(1)"},
		{"00000000-0000-0000-0000-0000000000ff": 1},
	} {
		h.must(bob, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": out["version"], "fields": bad}, 422)
	}

	// The activity log names the field and shows option names.
	out = h.must(bob, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": out["version"], "fields": map[string]any{str(vendor, "id"): delivered}}, 200)
	d := h.must(bob, "GET", "/api/v1/tasks/"+id, nil, 200)
	found := false
	for _, raw := range d["events"].([]any) {
		e := raw.(map[string]any)
		data, _ := e["data"].(map[string]any)
		if e["kind"] == "field" && data["field"] == "Stage" && data["from"] == "Ordered" && data["to"] == "Delivered" {
			found = true
		}
	}
	if !found {
		t.Fatalf("field event missing: %v", d["events"])
	}

	// Null clears; removing a select option clears it from tasks.
	out = h.must(bob, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": out["version"], "fields": map[string]any{str(cost, "id"): nil}}, 200)
	if _, ok := out["fields"].(map[string]any)[str(cost, "id")]; ok {
		t.Fatal("null should clear the value")
	}
	h.must(alice, "PATCH", "/api/v1/fields/"+str(vendor, "id"), map[string]any{"options": []map[string]any{{"id": ordered, "name": "Ordered"}}}, 200)
	got := h.must(bob, "GET", "/api/v1/tasks/"+id, nil, 200)
	if _, ok := got["fields"].(map[string]any)[str(vendor, "id")]; ok {
		t.Fatal("a removed option is cleared from tasks")
	}
	h.must(alice, "PATCH", "/api/v1/fields/"+str(vendor, "id"), map[string]any{"type": "text"}, 422)

	// Moving to another space drops the values; deleting a field is audited.
	other := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Other", "key": "OTF"}, 201)
	otherList := str(other["lists"].([]any)[0].(map[string]any), "id")
	got = h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": got["version"], "fields": map[string]any{str(link, "id"): "https://example.org/x"}}, 200)
	moved := h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": got["version"], "list_id": otherList}, 200)
	if len(moved["fields"].(map[string]any)) != 0 {
		t.Fatalf("values must not follow the task to another space: %v", moved["fields"])
	}
	h.must(bob, "DELETE", "/api/v1/fields/"+str(cost, "id"), nil, 403)
	h.must(alice, "DELETE", "/api/v1/fields/"+str(cost, "id"), nil, 204)
	audit := h.must(as{user: "admin", groups: "tasks-admin"}, "GET", "/api/v1/audit?entity=field", nil, 200)["entries"].([]any)
	if len(audit) != 1 {
		t.Fatalf("field delete audit: %v", audit)
	}
	h.must(as{user: "carol"}, "GET", "/api/v1/spaces/"+spaceID+"/fields", nil, 404)
}
