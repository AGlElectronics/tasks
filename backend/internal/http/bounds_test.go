package http

import (
	"context"
	"testing"
)

// TestTaskDetailIsBounded: the task page embeds only the newest comments
// and events; the rest is paged through /comments and /events.
func TestTaskDetailIsBounded(t *testing.T) {
	h := newHarness(t)
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	h.must(bob, "GET", "/api/v1/me", nil, 200)

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Bounds", "key": "BND"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "chatty"}, 201)
	id := str(task, "id")

	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO comments (task_id, author_id, body, created_at)
		SELECT $1, created_by, 'comment ' || g, now() - (300 - g) * interval '1 second'
		  FROM tasks, generate_series(1, 250) g WHERE tasks.id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO task_events (task_id, kind, created_at)
		SELECT $1, 'title', now() - (300 - g) * interval '1 second' FROM generate_series(1, 260) g`, id); err != nil {
		t.Fatal(err)
	}

	d := h.must(alice, "GET", "/api/v1/tasks/"+id, nil, 200)
	comments := d["comments"].([]any)
	if len(comments) != 100 || int(d["comments_total"].(float64)) != 250 {
		t.Fatalf("detail: %d comments of %v", len(comments), d["comments_total"])
	}
	if str(comments[99].(map[string]any), "body") != "comment 250" || str(comments[0].(map[string]any), "body") != "comment 151" {
		t.Fatalf("detail should hold the newest comments oldest first, got %v … %v", comments[0], comments[99])
	}
	if len(d["events"].([]any)) != 200 || int(d["events_total"].(float64)) != 261 {
		t.Fatalf("detail: %d events of %v", len(d["events"].([]any)), d["events_total"])
	}

	// Page through all comments, 100 at a time.
	seen, cursor, pages := 0, "", 0
	for {
		path := "/api/v1/tasks/" + id + "/comments?limit=100"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		rec, body := h.do(alice, "GET", path, nil)
		if rec.Code != 200 {
			t.Fatalf("page %d: %d %s", pages, rec.Code, rec.Body.String())
		}
		seen += len(body["comments"].([]any))
		pages++
		cursor = rec.Header().Get("X-Next-Cursor")
		if cursor == "" {
			break
		}
	}
	if seen != 250 || pages != 3 {
		t.Fatalf("paged %d comments in %d pages", seen, pages)
	}
	rec, body := h.do(alice, "GET", "/api/v1/tasks/"+id+"/events?limit=500", nil)
	if rec.Code != 200 || len(body["events"].([]any)) != 261 {
		t.Fatalf("events page: %d", rec.Code)
	}

	// Same access rule as the task: bob cannot see the closed space.
	h.must(bob, "GET", "/api/v1/tasks/"+id+"/comments", nil, 404)
	h.must(bob, "GET", "/api/v1/tasks/"+id+"/events", nil, 404)
	h.must(alice, "GET", "/api/v1/tasks/"+id+"/comments?cursor=nope", nil, 400)

	spaces := h.must(alice, "GET", "/api/v1/spaces", nil, 200)
	if spaces["truncated"] != false {
		t.Fatalf("a small sidebar is not truncated, got %v", spaces["truncated"])
	}
}
