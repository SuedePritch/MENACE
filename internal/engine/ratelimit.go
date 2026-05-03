package engine

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/flitsinc/go-llms/llms"
	mlog "menace/internal/log"
)

// RateLimiter tracks rate limit state shared across the architect and all workers
// hitting the same provider. Thread-safe.
type RateLimiter struct {
	mu        sync.Mutex
	throttled bool
	until     time.Time
}

// IsRateLimitError returns true and the retry delay if the error is an HTTP 429.
func IsRateLimitError(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}
	var httpErr *llms.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == 429 {
		// Default backoff: 30s. Providers often include a Retry-After header,
		// but go-llms doesn't surface it yet — use a safe default.
		return true, 30 * time.Second
	}
	return false, 0
}

// RecordRateLimit marks the limiter as throttled until now+duration.
func (rl *RateLimiter) RecordRateLimit(d time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(rl.until) {
		rl.until = until
	}
	rl.throttled = true
	mlog.Info("rate limit recorded", slog.Duration("wait", d))
}

// Throttled returns true and the remaining wait if currently rate limited.
func (rl *RateLimiter) Throttled() (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if !rl.throttled {
		return false, 0
	}
	remaining := time.Until(rl.until)
	if remaining <= 0 {
		rl.throttled = false
		return false, 0
	}
	return true, remaining
}

// Wait blocks until the rate limit window has passed or ctx is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	throttled, remaining := rl.Throttled()
	if !throttled {
		return nil
	}
	mlog.Info("rate limit wait", slog.Duration("remaining", remaining))
	select {
	case <-time.After(remaining):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
