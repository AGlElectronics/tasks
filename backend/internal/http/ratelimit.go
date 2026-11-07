package http

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/clientip"
)

// rateLimiter is a per-user token bucket (TASK-41). The limits are
// deliberately generous — they exist to stop a runaway script or agent
// loop from starving everyone else, not to throttle real use: a person
// cannot click 20 times a second for long, and the burst absorbs page
// loads that fan out into many requests.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   rate.Limit
	burst   int
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(perSecond float64, burst int) *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, limit: rate.Limit(perSecond), burst: burst}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		// Lazy cleanup: before growing a big map, drop buckets that have
		// been idle long enough to be full again anyway.
		if len(rl.buckets) > 10_000 {
			cutoff := time.Now().Add(-10 * time.Minute)
			for k, old := range rl.buckets {
				if old.seen.Before(cutoff) {
					delete(rl.buckets, k)
				}
			}
		}
		b = &bucket{lim: rate.NewLimiter(rl.limit, rl.burst)}
		rl.buckets[key] = b
	}
	b.seen = time.Now()
	return b.lim.Allow()
}

// middleware keys the bucket by the authenticated user, so one noisy
// client cannot slow anyone else down. It must run inside the auth group.
func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientip.FromRequest(r)
		if u, ok := auth.FromContext(r.Context()); ok {
			key = u.Issuer + "|" + u.Subject
		}
		if !rl.allow(key) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error": map[string]any{
					"code":    "rate_limited",
					"message": "too many requests — slow down and try again",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
