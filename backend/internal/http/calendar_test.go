package http

import "testing"

func TestCalendar(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Cal", "key": "CAL"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	mk := func(body map[string]any) map[string]any {
		body["list_id"] = listID
		return h.must(alice, "POST", "/api/v1/tasks", body, 201)
	}
	mk(map[string]any{"title": "in range", "due_at": "2027-03-10"})
	mk(map[string]any{"title": "spans in", "start_at": "2027-02-25", "due_at": "2027-03-02"})
	mk(map[string]any{"title": "outside", "due_at": "2027-05-01"})
	mk(map[string]any{"title": "undated"})
	mk(map[string]any{"title": "bob's", "due_at": "2027-03-15", "assignee_ids": []string{bobID}})

	got := h.must(alice, "GET", "/api/v1/calendar?from=2027-03-01&to=2027-03-31", nil, 200)
	titles := map[string]bool{}
	for _, raw := range got["tasks"].([]any) {
		titles[str(raw.(map[string]any), "title")] = true
	}
	if len(titles) != 3 || !titles["in range"] || !titles["spans in"] || !titles["bob's"] || got["truncated"] != false {
		t.Fatalf("calendar: %v", titles)
	}

	// Bob sees only the task he is assigned to; mine=1 narrows alice's.
	if n := len(h.must(bob, "GET", "/api/v1/calendar?from=2027-03-01&to=2027-03-31", nil, 200)["tasks"].([]any)); n != 1 {
		t.Fatalf("bob sees %d", n)
	}
	if n := len(h.must(alice, "GET", "/api/v1/calendar?from=2027-03-01&to=2027-03-31&mine=1", nil, 200)["tasks"].([]any)); n != 0 {
		t.Fatalf("alice's own: %d", n)
	}

	h.must(alice, "GET", "/api/v1/calendar?from=2027-03-01", nil, 422)
	h.must(alice, "GET", "/api/v1/calendar?from=2027-03-01&to=2027-12-31", nil, 422)
	h.must(alice, "GET", "/api/v1/calendar?from=2027-03-10&to=2027-03-01", nil, 422)
}
