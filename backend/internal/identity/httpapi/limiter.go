package httpapi

import (
	"sync"
	"time"
)

const limiterWindow = time.Minute
const limiterMaxClients = 4096

// limiter is a small in-process admission control: a per-client attempt limit
// within a sliding minute plus a global concurrency limit.
type limiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	active      int
	now         func() time.Time
	maxAttempts int
	maxActive   int
}

func newLimiter(maxAttempts, maxActive int) *limiter {
	return &limiter{attempts: map[string][]time.Time{}, now: time.Now, maxAttempts: maxAttempts, maxActive: maxActive}
}

func (l *limiter) admitAttempt(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.attempts) >= limiterMaxClients {
		for k, v := range l.attempts {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= limiterWindow {
				delete(l.attempts, k)
			}
		}
	}
	if _, ok := l.attempts[ip]; !ok && len(l.attempts) >= limiterMaxClients {
		return false
	}
	v := l.attempts[ip][:0]
	for _, t := range l.attempts[ip] {
		if now.Sub(t) < limiterWindow {
			v = append(v, t)
		}
	}
	if len(v) >= l.maxAttempts {
		l.attempts[ip] = v
		return false
	}
	l.attempts[ip] = append(v, now)
	return true
}

func (l *limiter) acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active >= l.maxActive {
		return false
	}
	l.active++
	return true
}

func (l *limiter) release() { l.mu.Lock(); l.active--; l.mu.Unlock() }
