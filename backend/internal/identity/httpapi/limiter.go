package httpapi

import (
	"sync"
	"time"
)

const limiterWindow = time.Minute
const limiterMaxClients = 4096

type limiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	now         func() time.Time
	maxAttempts int
	window      time.Duration
}

func newLimiter(maxAttempts int) *limiter {
	return newWindowLimiter(maxAttempts, limiterWindow)
}

// newWindowLimiter builds the same in-process limiter with an explicit window,
// for buckets that are wider than the per-minute request throttle (for example
// per-target password-reset issuance).
func newWindowLimiter(maxAttempts int, window time.Duration) *limiter {
	return &limiter{attempts: map[string][]time.Time{}, now: time.Now, maxAttempts: maxAttempts, window: window}
}

func (l *limiter) admitAttempt(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.attempts) >= limiterMaxClients {
		for k, v := range l.attempts {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= l.window {
				delete(l.attempts, k)
			}
		}
	}
	if _, ok := l.attempts[key]; !ok && len(l.attempts) >= limiterMaxClients {
		return false
	}
	v := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if now.Sub(at) < l.window {
			v = append(v, at)
		}
	}
	if len(v) >= l.maxAttempts {
		l.attempts[key] = v
		return false
	}
	l.attempts[key] = append(v, now)
	return true
}
