package agentapi

import (
	"sync"
	"time"
)

// limiter is a token bucket per key: each key may make rate calls a minute,
// in a burst of up to rate. It is per instance: in a cluster each instance
// keeps its own, so a token may make up to the rate on every instance.
type limiter struct {
	rate float64 // calls a minute
	now  func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{rate: float64(perMinute), now: time.Now, buckets: map[string]*bucket{}}
}

// allow takes one call from key's bucket. When there is none it reports how
// long until there is.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.rate, at: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.at).Minutes() * l.rate
	if b.tokens > l.rate {
		b.tokens = l.rate
	}
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Minute))
	return false, wait.Round(time.Second) + time.Second
}

// sweep forgets buckets that have been full for a while, so the map does
// not grow with every token ever seen.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < 10*time.Minute {
		return
	}
	l.swept = now
	for k, b := range l.buckets {
		if now.Sub(b.at) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}
