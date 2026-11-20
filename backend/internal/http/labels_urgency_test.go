package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/denlink"
)

// titles lists the titles of a {"tasks": [...]} response in order.
func titles(t *testing.T, body map[string]any, key string) []string {
	t.Helper()
	items, _ := body[key].([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		m := it.(map[string]any)
		if v := str(m, "title"); v != "" {
			out = append(out, v)
		} else {
			out = append(out, str(m, "task_title"))
		}
	}
	return out
}

func sameTitles(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestLabelsAndUrgencyFilters covers the label (tag) and urgency (priority)
// filters on list, My Work and Inbox reads, the urgency sort, and the
// urgency order of the portal to-dos.
func TestLabelsAndUrgencyFilters(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Fest", "key": "FEST"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	venue := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/tags", map[string]any{"name": "Venue"}, 201)
	press := h.must(alice, "POST", "/api/v1/spaces/"+spaceID+"/tags", map[string]any{"name": "Press"}, 201)

	mk := func(title string, priority any, due string, tags ...string) map[string]any {
		t.Helper()
		body := map[string]any{"list_id": listID, "title": title}
		if priority != nil {
			body["priority"] = priority
		}
		if due != "" {
			body["due_at"] = due
		}
		task := h.must(alice, "POST", "/api/v1/tasks", body, 201)
		// Assigning in a patch notifies bob; assigning at creation does not.
		patch := map[string]any{"version": task["version"], "assignee_ids": []string{bobID}}
		if len(tags) > 0 {
			patch["tag_ids"] = tags
		}
		return h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), patch, 200)
	}
	mk("Plain", nil, todayPlus(h, 1))
	mk("Low press", "low", todayPlus(h, 2), str(press, "id"))
	mk("Normal venue", "normal", todayPlus(h, 3), str(venue, "id"))
	mk("Urgent venue", "urgent", todayPlus(h, 9), str(venue, "id"), str(press, "id"))
	mk("High press", "high", todayPlus(h, 5), str(press, "id"))

	check := func(label string, p as, path string, key string, want ...string) {
		t.Helper()
		got := titles(t, h.must(p, "GET", path, nil, 200), key)
		if !sameTitles(got, want) {
			t.Fatalf("%s: got %v want %v", label, got, want)
		}
	}

	// Lists keep board order and filter by urgency and label.
	list := "/api/v1/lists/" + listID + "/tasks"
	check("list unfiltered", alice, list, "tasks", "Plain", "Low press", "Normal venue", "Urgent venue", "High press")
	check("list urgent", alice, list+"?priority=urgent", "tasks", "Urgent venue")
	check("list urgent or high", alice, list+"?priority=urgent,high", "tasks", "Urgent venue", "High press")
	check("list repeated priority", alice, list+"?priority=urgent&priority=high", "tasks", "Urgent venue", "High press")
	check("list none", alice, list+"?priority=none", "tasks", "Plain")
	check("list label by id", alice, list+"?tag="+str(venue, "id"), "tasks", "Normal venue", "Urgent venue")
	check("list label by name", alice, list+"?tag=press", "tasks", "Low press", "Urgent venue", "High press")
	check("list either label", alice, list+"?tag=Venue,Press", "tasks", "Low press", "Normal venue", "Urgent venue", "High press")
	check("list urgency and label", alice, list+"?priority=high,low&tag=press", "tasks", "Low press", "High press")
	check("list no match", alice, list+"?priority=urgent&tag="+url.QueryEscape("nope"), "tasks")

	// My Work: same filters, plus sort=urgency.
	check("my-work due order", bob, "/api/v1/my-work", "tasks",
		"Plain", "Low press", "Normal venue", "High press", "Urgent venue")
	check("my-work urgency order", bob, "/api/v1/my-work?sort=urgency", "tasks",
		"Urgent venue", "High press", "Normal venue", "Low press", "Plain")
	check("my-work label", bob, "/api/v1/my-work?tag=Venue&sort=urgency", "tasks", "Urgent venue", "Normal venue")
	check("my-work urgency filter", bob, "/api/v1/my-work?priority=urgent,high", "tasks", "High press", "Urgent venue")

	// Bad values are validation errors, not empty results.
	h.must(bob, "GET", "/api/v1/my-work?priority=asap", nil, 422)
	h.must(bob, "GET", "/api/v1/my-work?sort=random", nil, 422)
	h.must(alice, "GET", list+"?priority=asap", nil, 422)
	h.must(bob, "GET", "/api/v1/inbox?priority=asap", nil, 422)

	// Inbox: assignments notified bob; the filter follows the task.
	check("inbox unfiltered count", bob, "/api/v1/inbox", "notifications",
		"High press", "Urgent venue", "Normal venue", "Low press", "Plain")
	check("inbox urgent", bob, "/api/v1/inbox?priority=urgent", "notifications", "Urgent venue")
	check("inbox label", bob, "/api/v1/inbox?tag=venue", "notifications", "Urgent venue", "Normal venue")
	inbox := h.must(bob, "GET", "/api/v1/inbox?priority=urgent", nil, 200)["notifications"].([]any)
	first := inbox[0].(map[string]any)
	if first["priority"] != "urgent" || len(first["tags"].([]any)) != 2 {
		t.Fatalf("inbox row should carry the task's urgency and labels: %v", first)
	}

	// Portal to-dos stay task-backed and put urgent work first.
	const cred = "module-credential-for-den"
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO den_link (den_base_url, credential, bundle)
		VALUES ('http://den', $1, '{"brand":{},"settings":{"data":{}}}')`, cred); err != nil {
		t.Fatal(err)
	}
	den := denlink.New(h.pool, denlink.Config{Slug: "tasks", AppEnv: "development"}, zerolog.Nop())
	if err := den.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	h.deps.Config.PublicBaseURL = "https://tasks.example.org"
	h.rebuild(func(d *Deps) { d.Den = den })

	req := httptest.NewRequest(http.MethodGet, "/api/portal/todos", nil)
	req.Header.Set("Authorization", "Bearer "+cred)
	req.Header.Set("X-Den-User-Issuer", "dev")
	req.Header.Set("X-Den-User-Subject", "bob")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("todos: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Title, Detail, Severity string
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range body.Items {
		got = append(got, it.Title)
	}
	want := []string{"Urgent venue", "High press", "Normal venue", "Low press", "Plain"}
	if !sameTitles(got, want) {
		t.Fatalf("portal order: got %v want %v", got, want)
	}
	if body.Items[0].Severity != "warning" || body.Items[0].Detail[:6] != "Urgent" {
		t.Fatalf("urgent item should be flagged: %+v", body.Items[0])
	}
	if body.Items[2].Severity != "info" {
		t.Fatalf("normal item stays info: %+v", body.Items[2])
	}
}
