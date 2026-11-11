package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/realtime"
)

// Live updates (Server-Sent Events) at GET /api/v1/stream.
//
// The stream says WHAT changed — a list, a space, the person's inbox — and
// never the content; the browser refetches what it shows through the normal,
// access-checked API. A list or space is only announced to people who can
// see it (or could see it just before, so a removed space disappears).
const (
	streamHeartbeat = 25 * time.Second // below nginx's 60 s proxy_read_timeout
	streamRefresh   = 2 * time.Minute  // re-check what the person can see
	streamMaxAge    = 30 * time.Minute // then the browser reconnects (fresh token)
	streamPerUser   = 5
)

// streamEvent is one SSE data payload.
type streamEvent struct {
	Type    string     `json:"type"` // list, space, inbox, resync
	ListID  *uuid.UUID `json:"list_id,omitempty"`
	SpaceID *uuid.UUID `json:"space_id,omitempty"`
}

// streamLimiter caps open streams per person.
type streamLimiter struct {
	mu   sync.Mutex
	open map[uuid.UUID]int
}

func (l *streamLimiter) acquire(id uuid.UUID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open == nil {
		l.open = map[uuid.UUID]int{}
	}
	if l.open[id] >= streamPerUser {
		return false
	}
	l.open[id]++
	return true
}

func (l *streamLimiter) release(id uuid.UUID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[id]--; l.open[id] <= 0 {
		delete(l.open, id)
	}
}

func (h *taskHandler) stream(w http.ResponseWriter, r *http.Request) {
	u, err := caller(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.hub == nil {
		httperror.Render(w, r, httperror.New(httperror.CodeUnavailable, "live updates are not available"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httperror.Render(w, r, httperror.New(httperror.CodeUnavailable, "streaming is not supported here"))
		return
	}
	if !h.streams.acquire(u.ID) {
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
			"code":    httperror.CodeRateLimited,
			"message": fmt.Sprintf("at most %d live-update connections per person; close other tabs", streamPerUser),
		}})
		return
	}
	defer h.streams.release(u.ID)

	ctx := r.Context()
	vis, err := h.visibility(ctx, u, true)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	sub := h.hub.Subscribe()
	defer h.hub.Unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer
	w.WriteHeader(http.StatusOK)

	send := func(ev streamEvent) bool {
		raw, _ := json.Marshal(ev)
		if _, err := fmt.Fprintf(w, "event: change\ndata: %s\n\n", raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if _, err := fmt.Fprintf(w, "retry: 5000\nevent: ready\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()
	refresh := time.NewTicker(streamRefresh)
	defer refresh.Stop()
	maxAge := time.NewTimer(streamMaxAge)
	defer maxAge.Stop()
	lastRecompute := time.Now()
	recompute := func(force bool) {
		if !force && time.Since(lastRecompute) < 5*time.Second {
			return
		}
		if next, err := h.visibility(ctx, u, true); err == nil {
			vis = next
			lastRecompute = time.Now()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-maxAge.C:
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-refresh.C:
			recompute(true)
		case c := <-sub.C:
			if sub.TakeOverflow() {
				c = realtime.Change{Kind: "resync"}
			}
			ok := true
			switch c.Kind {
			case "list":
				if !vis.lists[c.ListID] && vis.spaces[c.SpaceID] {
					// Possibly a list created since the last check.
					recompute(false)
				}
				if vis.lists[c.ListID] {
					ok = send(streamEvent{Type: "list", ListID: &c.ListID, SpaceID: &c.SpaceID})
				}
			case "space":
				before := vis.spaces[c.SpaceID]
				recompute(true)
				if before || vis.spaces[c.SpaceID] {
					ok = send(streamEvent{Type: "space", SpaceID: &c.SpaceID})
				}
			case "inbox":
				if c.UserID == u.ID {
					// An assignment opens a task: see its list from now on.
					recompute(false)
					ok = send(streamEvent{Type: "inbox"})
				}
			case "resync":
				recompute(true)
				ok = send(streamEvent{Type: "resync"})
			}
			if !ok {
				return
			}
		}
	}
}

// streamStatus is GET /api/v1/stream/status: whether live updates are on
// (the web app polls more often when they are not).
func (h *taskHandler) streamStatus(_ *http.Request, _ *auth.User) (int, any, error) {
	return http.StatusOK, map[string]any{"enabled": h.hub != nil}, nil
}
