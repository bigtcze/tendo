package security

import "sync"

// PasswordGate bounds simultaneous Argon2 work across authentication flows.
type PasswordGate struct {
	mu            sync.Mutex
	active, limit int
}

func NewPasswordGate(limit int) *PasswordGate {
	if limit < 1 {
		limit = 1
	}
	return &PasswordGate{limit: limit}
}
func (g *PasswordGate) Acquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.limit {
		return false
	}
	g.active++
	return true
}
func (g *PasswordGate) Release()        { g.mu.Lock(); g.active--; g.mu.Unlock() }
func (g *PasswordGate) Active() int     { g.mu.Lock(); defer g.mu.Unlock(); return g.active }
func (g *PasswordGate) Saturated() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.active >= g.limit }
func (g *PasswordGate) HashPassword(password string) (string, error) {
	if !g.Acquire() {
		return "", ErrPasswordWorkLimit
	}
	defer g.Release()
	return HashPassword(password)
}
func (g *PasswordGate) VerifyPassword(encoded, password string) (bool, error) {
	if !g.Acquire() {
		return false, ErrPasswordWorkLimit
	}
	defer g.Release()
	return VerifyPassword(encoded, password)
}
