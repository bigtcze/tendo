package httpapi

import (
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

func setupWithGate(g *security.PasswordGate) error {
	if !g.Acquire() {
		return security.ErrPasswordWorkLimit
	}
	defer g.Release()
	_, err := security.HashPassword("setup gate passphrase sufficiently long")
	if err != nil {
		return err
	}
	return security.ErrPasswordWorkLimit
}
func verifyWithGate(g *security.PasswordGate, hash string) error {
	if !g.Acquire() {
		return security.ErrPasswordWorkLimit
	}
	defer g.Release()
	if _, err := security.VerifyPassword(hash, "sufficiently long login password"); err != nil {
		return err
	}
	return security.ErrPasswordWorkLimit
}
func acceptWithGate(g *security.PasswordGate, _ string) error {
	if !g.Acquire() {
		return security.ErrPasswordWorkLimit
	}
	defer g.Release()
	_, err := security.HashPassword("invitation gate passphrase sufficiently long")
	if err != nil {
		return err
	}
	return security.ErrPasswordWorkLimit
}
func TestPasswordGateSharedAcrossSetupLoginAndInvitation(t *testing.T) {
	gate := security.NewPasswordGate(1)
	saturated, err := gate.HashPassword("sufficiently long saturation password")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{"setup": func() error { return setupWithGate(gate) }, "login": func() error { return verifyWithGate(gate, saturated) }, "invitation": func() error { return acceptWithGate(gate, saturated) }}
	for name, run := range checks {
		if err := run(); !errors.Is(err, security.ErrPasswordWorkLimit) {
			t.Fatalf("%s did not share gate: %v", name, err)
		}
		if gate.Active() != 0 {
			t.Fatalf("%s leaked gate slot", name)
		}
	}
}
