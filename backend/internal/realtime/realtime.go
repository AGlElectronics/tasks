// Package realtime turns database change announcements into live updates
// for browsers (Server-Sent Events).
//
// Triggers (migration 20261111000005_realtime.sql) send small payloads on
// the PostgreSQL channel tasks_changes naming which list, space or inbox
// changed. Hub keeps one LISTEN connection, decodes them and hands each to
// every subscriber; subscribers decide what their person may know about.
// Because the source is PostgreSQL, every backend replica sees every change.
package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Channel is the PostgreSQL NOTIFY channel the triggers use.
const Channel = "tasks_changes"

// Change is one announcement. Kind is list, space or inbox.
type Change struct {
	Kind    string    `json:"k"`
	ListID  uuid.UUID `json:"l,omitempty"`
	SpaceID uuid.UUID `json:"s,omitempty"`
	UserID  uuid.UUID `json:"u,omitempty"`
}

// Subscription receives changes until it is closed.
type Subscription struct {
	C chan Change
	// overflow is set when changes were dropped because the subscriber was
	// too slow; it should tell the browser to refetch everything.
	overflow bool
	mu       sync.Mutex
}

// TakeOverflow reports and clears the overflow flag.
func (s *Subscription) TakeOverflow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.overflow
	s.overflow = false
	return o
}

// Hub fans changes out to subscribers.
type Hub struct {
	pool *pgxpool.Pool
	log  zerolog.Logger

	mu   sync.Mutex
	subs map[*Subscription]struct{}
	// ready is closed once the first LISTEN succeeded (tests wait on it).
	ready     chan struct{}
	readyOnce sync.Once
}

// NewHub returns a Hub; call Run to start listening.
func NewHub(pool *pgxpool.Pool, log zerolog.Logger) *Hub {
	return &Hub{pool: pool, log: log, subs: map[*Subscription]struct{}{}, ready: make(chan struct{})}
}

// Ready is closed once the hub listens.
func (h *Hub) Ready() <-chan struct{} { return h.ready }

// Subscribe registers a subscriber. Call Unsubscribe when done.
func (h *Hub) Subscribe() *Subscription {
	s := &Subscription{C: make(chan Change, 64)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// Subscribers is the number of open subscriptions.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Publish hands a change to every subscriber without blocking; a full
// subscriber is marked as overflowed instead.
func (h *Hub) Publish(c Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		select {
		case s.C <- c:
		default:
			s.mu.Lock()
			s.overflow = true
			s.mu.Unlock()
		}
	}
}

// Run LISTENs until ctx ends, reconnecting with a growing pause after
// errors.
func (h *Hub) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		h.log.Warn().Err(err).Dur("retry_in", backoff).Msg("realtime: listen failed")
		// Changes may have been missed while disconnected.
		h.Publish(Change{Kind: "resync"})
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	h.readyOnce.Do(func() { close(h.ready) })
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			// The connection may be mid-LISTEN; do not hand it back.
			_ = conn.Conn().Close(context.Background())
			return err
		}
		var c Change
		if json.Unmarshal([]byte(n.Payload), &c) == nil && c.Kind != "" {
			h.Publish(c)
		}
	}
}
