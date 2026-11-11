package realtime

import (
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestPublishFansOutAndFlagsSlowSubscribers(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	fast, slow := h.Subscribe(), h.Subscribe()
	if h.Subscribers() != 2 {
		t.Fatalf("subscribers: %d", h.Subscribers())
	}
	list := uuid.New()
	for i := 0; i < cap(slow.C)+5; i++ {
		h.Publish(Change{Kind: "list", ListID: list})
		select {
		case c := <-fast.C:
			if c.ListID != list {
				t.Fatal("wrong change")
			}
		default:
			t.Fatal("fast subscriber missed a change")
		}
	}
	if fast.TakeOverflow() {
		t.Fatal("a subscriber that keeps up never overflows")
	}
	if !slow.TakeOverflow() || slow.TakeOverflow() {
		t.Fatal("overflow is reported once")
	}
	h.Unsubscribe(slow)
	h.Unsubscribe(fast)
	if h.Subscribers() != 0 {
		t.Fatal("unsubscribe")
	}
}
