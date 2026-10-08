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
}

func newLimiter(maxAttempts int) *limiter {
	return &limiter{attempts: map[string][]time.Time{}, now: time.Now, maxAttempts: maxAttempts}
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
	for _, at := range l.attempts[ip] {
		if now.Sub(at) < limiterWindow {
			v = append(v, at)
		}
	}
	if len(v) >= l.maxAttempts {
		l.attempts[ip] = v
		return false
	}
	l.attempts[ip] = append(v, now)
	return true
}
