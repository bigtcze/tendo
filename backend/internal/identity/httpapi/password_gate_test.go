package httpapi

import (
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

func setupWithGate(g *security.PasswordGate) error {
	_, err := g.HashPassword("setup gate sufficiently long password")
	return err
}
func verifyWithGate(g *security.PasswordGate, hash string) error {
	_, err := g.VerifyPassword(hash, "login sufficiently long password")
	return err
}
func acceptWithGate(g *security.PasswordGate, _ string) error {
	_, err := g.HashPassword("invitation sufficiently long password")
	return err
}
func TestPasswordGateSharedAcrossSetupLoginAndInvitation(t *testing.T) {
	gate := security.NewPasswordGate(1)
	saturated, err := gate.HashPassword("sufficiently long saturation password")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{"setup": func() error { return setupWithGate(gate) }, "login": func() error { return verifyWithGate(gate, saturated) }, "invitation": func() error { return acceptWithGate(gate, saturated) }}
	if !gate.Acquire() {
		t.Fatal("could not occupy the single gate slot")
	}
	for name, run := range checks {
		if err := run(); !errors.Is(err, security.ErrPasswordWorkLimit) {
			t.Fatalf("%s did not share gate: %v", name, err)
		}
		if gate.Active() != 1 {
			t.Fatalf("%s changed gate occupancy while rejected", name)
		}
	}
	gate.Release()
	for name, run := range checks {
		if err := run(); errors.Is(err, security.ErrPasswordWorkLimit) {
			t.Fatalf("%s rejected with a free slot: %v", name, err)
		}
		if gate.Active() != 0 {
			t.Fatalf("%s leaked gate slot", name)
		}
	}
}
