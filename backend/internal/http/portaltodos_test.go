package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/denlink"
)

func TestPortalTodos(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Crew", "key": "CREW"}, 201)
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")
	done := statusByCategory(sp, "done")

	open := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "Pack radios", "assignee_ids": []string{bobID},
		"due_at": todayPlus(h, -1),
	}, 201)
	later := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "Count badges", "assignee_ids": []string{bobID},
		"due_at": todayPlus(h, 10),
	}, 201)
	finished := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "Done already", "assignee_ids": []string{bobID},
	}, 201)
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(finished, "id"), map[string]any{
		"status_id": done, "version": finished["version"],
	}, 200)
	trashed := h.must(alice, "POST", "/api/v1/tasks", map[string]any{
		"list_id": listID, "title": "Thrown out", "assignee_ids": []string{bobID},
	}, 201)
	h.must(alice, "DELETE", "/api/v1/tasks/"+str(trashed, "id"), nil, 204)

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

	get := func(auth string, hdr bool) *httptest.ResponseRecorder {
		t.Helper()
		path := "/api/portal/todos"
		if !hdr {
			path += "?issuer=dev&subject=bob"
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if hdr {
			req.Header.Set("X-Den-User-Issuer", "dev")
			req.Header.Set("X-Den-User-Subject", "bob")
		}
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("", true); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credential: %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("Bearer wrong-credential", true); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credential: %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/portal/todos", nil)
	req.Header.Set("Authorization", "Bearer "+cred)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing person: %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("Bearer "+cred, false); rec.Code != http.StatusOK {
		t.Fatalf("query form: %d %s", rec.Code, rec.Body.String())
	}

	rec = get("Bearer "+cred, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("todos: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID, Title, Detail, URL, Severity string
			DueAt                            *string `json:"due_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("want the two open tasks, got %+v", body.Items)
	}
	if body.Items[0].ID != str(open, "key") || body.Items[0].Severity != "danger" || body.Items[0].Title != "Pack radios" {
		t.Fatalf("overdue first: %+v", body.Items[0])
	}
	if body.Items[0].URL != "https://tasks.example.org/t/"+body.Items[0].ID || body.Items[0].DueAt == nil {
		t.Fatalf("link and due date: %+v", body.Items[0])
	}
	if body.Items[1].ID != str(later, "key") || body.Items[1].Severity != "info" {
		t.Fatalf("later task: %+v", body.Items[1])
	}
	if body.Items[0].Detail == "" {
		t.Fatal("detail should name the list and space")
	}

	if _, err := h.pool.Exec(ctx, `UPDATE den_link SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	if rec := get("Bearer "+cred, true); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked credential: %d %s", rec.Code, rec.Body.String())
	}
}
