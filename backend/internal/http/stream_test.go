package http

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/tasks/backend/internal/realtime"
)

// sseClient reads change events from GET /api/v1/stream.
type sseClient struct {
	t      *testing.T
	events chan streamEvent
	cancel context.CancelFunc
}

func openStream(t *testing.T, base string, p as) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/stream", nil)
	req.Header.Set("X-Dev-User", p.user)
	req.Header.Set("X-Dev-Groups", p.groups)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		cancel()
		t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	c := &sseClient{t: t, events: make(chan streamEvent, 100), cancel: cancel}
	ready := make(chan struct{})
	go func() {
		defer func() { _ = res.Body.Close() }()
		sc := bufio.NewScanner(res.Body)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				switch event {
				case "ready":
					close(ready)
				case "change":
					var ev streamEvent
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil {
						c.events <- ev
					}
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("stream never got ready")
	}
	t.Cleanup(cancel)
	return c
}

// expect waits for an event of the given type (and list, when set).
func (c *sseClient) expect(typ, listID string) {
	c.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if ev.Type == typ && (listID == "" || (ev.ListID != nil && ev.ListID.String() == listID)) {
				return
			}
		case <-deadline:
			c.t.Fatalf("no %s event (list %s)", typ, listID)
		}
	}
}

// quiet asserts that no list event for listID arrives within a short wait.
func (c *sseClient) quiet(listID string) {
	c.t.Helper()
	deadline := time.After(700 * time.Millisecond)
	for {
		select {
		case ev := <-c.events:
			if ev.Type == "list" && ev.ListID != nil && ev.ListID.String() == listID {
				c.t.Fatalf("unexpected list event for %s", listID)
			}
		case <-deadline:
			return
		}
	}
}

func TestLiveUpdates(t *testing.T) {
	h := newHarness(t)
	// Wait until the hub releases its connection. Closing the pool does not
	// wait for a connection this goroutine still holds.
	ctx, cancel := context.WithCancel(context.Background())
	hub := realtime.NewHub(h.pool, zerolog.Nop())
	stopped := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	select {
	case <-hub.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("hub not listening")
	}
	h.rebuild(func(d *Deps) { d.Hub = hub })
	srv := httptest.NewServer(h.router)
	t.Cleanup(srv.Close)

	alice := as{user: "alice", groups: "tasks-creator"}
	bob := as{user: "bob"}
	bobID := str(h.must(bob, "GET", "/api/v1/me", nil, 200), "id")
	h.must(as{user: "carol"}, "GET", "/api/v1/me", nil, 200)
	if st := h.must(alice, "GET", "/api/v1/stream/status", nil, 200); st["enabled"] != true {
		t.Fatalf("status: %v", st)
	}

	sp := h.must(alice, "POST", "/api/v1/spaces", map[string]any{"name": "Live", "key": "LIV"}, 201)
	spaceID := str(sp, "id")
	listID := str(sp["lists"].([]any)[0].(map[string]any), "id")

	a := openStream(t, srv.URL, alice)
	b := openStream(t, srv.URL, bob)

	// A change in a closed space reaches only those who can see it.
	task := h.must(alice, "POST", "/api/v1/tasks", map[string]any{"list_id": listID, "title": "live"}, 201)
	a.expect("list", listID)
	b.quiet(listID)

	// Sharing the space: bob hears about the space, then about its lists.
	h.must(alice, "PUT", "/api/v1/spaces/"+spaceID+"/grants", map[string]any{"subject_type": "user", "subject": bobID, "level": "view"}, 200)
	b.expect("space", "")
	h.must(alice, "POST", "/api/v1/tasks/"+str(task, "id")+"/comments", map[string]any{"body": "now you see it"}, 201)
	b.expect("list", listID)

	// Notifications are announced only to their owner.
	h.must(alice, "PATCH", "/api/v1/tasks/"+str(task, "id"), map[string]any{"assignee_ids": []string{bobID}, "version": task["version"]}, 200)
	b.expect("inbox", "")

	// A sixth stream for the same person is refused.
	for i := 0; i < 4; i++ {
		openStream(t, srv.URL, as{user: "carol"})
	}
	openStream(t, srv.URL, as{user: "carol"})
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/stream", nil)
	req.Header.Set("X-Dev-User", "carol")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 429 {
		t.Fatalf("6th stream: %d", res.StatusCode)
	}
}

func TestStreamOffWithoutHub(t *testing.T) {
	h := newHarness(t)
	h.must(as{user: "alice"}, "GET", "/api/v1/stream", nil, 503)
	if st := h.must(as{user: "alice"}, "GET", "/api/v1/stream/status", nil, 200); st["enabled"] != false {
		t.Fatalf("status: %v", st)
	}
}
