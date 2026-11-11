package http

import "testing"

func TestTimeTracking(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	carol := as{user: "carol"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")
	h.must(carol, "GET", "/api/v1/me", nil, 200)

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Time", "key": "TIM"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	h.must(alice, "PUT", "/api/v1/spaces/"+str(sp, "id")+"/grants", map[string]any{"subject_type": "user", "subject": bobID, "level": "edit"}, 200)
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "Build stage"}, 201)
	id := str(task, "id")

	task = h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": task["version"], "estimate_minutes": 240}, 200)
	if task["estimate_minutes"] != float64(240) {
		t.Fatalf("estimate: %v", task["estimate_minutes"])
	}
	h.must(alice, "PATCH", "/api/v1/tasks/"+id, map[string]any{"version": task["version"], "estimate_minutes": -5}, 422)

	e := h.must(bob, "POST", "/api/v1/tasks/"+id+"/time", map[string]any{"minutes": 90, "spent_on": "2027-01-10", "note": "frame"}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+id+"/time", map[string]any{"minutes": 30}, 201)
	h.must(alice, "POST", "/api/v1/tasks/"+id+"/time", map[string]any{"minutes": 0}, 422)
	h.must(alice, "POST", "/api/v1/tasks/"+id+"/time", map[string]any{"minutes": 2000}, 422)
	h.must(carol, "POST", "/api/v1/tasks/"+id+"/time", map[string]any{"minutes": 10}, 404)

	got := h.must(bob, "GET", "/api/v1/tasks/"+id, nil, 200)
	if got["time_spent_minutes"] != float64(120) || len(got["time_entries"].([]any)) != 2 {
		t.Fatalf("task: spent %v entries %v", got["time_spent_minutes"], got["time_entries"])
	}
	lst := h.must(bob, "GET", "/api/v1/tasks/"+id+"/time", nil, 200)
	if lst["total_minutes"] != float64(120) {
		t.Fatalf("time list: %v", lst)
	}

	rep := h.must(bob, "GET", "/api/v1/lists/"+listID+"/time-report?from=2027-01-01&to=2027-01-31", nil, 200)
	people := rep["people"].([]any)
	if rep["total_minutes"] != float64(90) || len(people) != 1 || people[0].(map[string]any)["user_name"] != "Bob" {
		t.Fatalf("report: %v", rep)
	}
	h.must(bob, "GET", "/api/v1/lists/"+listID+"/time-report?from=jan", nil, 422)

	// Only the logger or a manager removes an entry.
	aliceEntry := str(lst["entries"].([]any)[0].(map[string]any), "id")
	if lst["entries"].([]any)[0].(map[string]any)["user_name"] != "Alice" {
		aliceEntry = str(lst["entries"].([]any)[1].(map[string]any), "id")
	}
	h.must(bob, "DELETE", "/api/v1/time/"+aliceEntry, nil, 403)
	h.must(bob, "DELETE", "/api/v1/time/"+str(e, "id"), nil, 204)
	h.must(alice, "DELETE", "/api/v1/time/"+aliceEntry, nil, 204)
	if got := h.must(bob, "GET", "/api/v1/tasks/"+id, nil, 200); got["time_spent_minutes"] != float64(0) {
		t.Fatalf("after deletes: %v", got["time_spent_minutes"])
	}
}
