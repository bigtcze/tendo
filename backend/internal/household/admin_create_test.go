package household

import (
	"bytes"
	"errors"
	"testing"
)

func TestHouseholdCreationFingerprintIsStableAndCanonical(t *testing.T) {
	actor := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B62"
	admin := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B63"
	base := householdCreationFingerprint(actor, "  Veselí  ", "Europe/Prague", admin)

	if len(base) != 32 {
		t.Fatalf("fingerprint length=%d want 32", len(base))
	}
	if again := householdCreationFingerprint(actor, "Veselí", "Europe/Prague", admin); !bytes.Equal(base, again) {
		t.Fatal("fingerprint is not deterministic or does not trim the name")
	}
	if lowered := householdCreationFingerprint("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62", "Veselí", "Europe/Prague",
		"0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"); !bytes.Equal(base, lowered) {
		t.Fatal("fingerprint is not case-insensitive in the actor and admin identifiers")
	}

	for name, changed := range map[string][]byte{
		"actor":          householdCreationFingerprint("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64", "Veselí", "Europe/Prague", admin),
		"name":           householdCreationFingerprint(actor, "Veselí 2", "Europe/Prague", admin),
		"timezone case":  householdCreationFingerprint(actor, "Veselí", "europe/prague", admin),
		"timezone value": householdCreationFingerprint(actor, "Veselí", "Europe/Berlin", admin),
		"admin user":     householdCreationFingerprint(actor, "Veselí", "Europe/Prague", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64"),
	} {
		if bytes.Equal(base, changed) {
			t.Fatalf("%s did not change the fingerprint", name)
		}
	}

	// Length prefixing keeps field boundaries unambiguous.
	left := householdCreationFingerprint(actor, "ab", "c", admin)
	right := householdCreationFingerprint(actor, "a", "bc", admin)
	if bytes.Equal(left, right) {
		t.Fatal("adjoining fields collided across the length prefix")
	}
}

func TestValidateHouseholdCreationNamesCreateFieldsInOrder(t *testing.T) {
	if _, err := validateHouseholdCreation("  Home  ", "Europe/Prague"); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	trimmed, err := validateHouseholdCreation("  Home  ", "Europe/Prague")
	if err != nil || trimmed != "Home" {
		t.Fatalf("trimmed=%q err=%v", trimmed, err)
	}
	for _, tc := range []struct {
		name, timezone, field, code string
	}{
		{name: "", timezone: "nowhere", field: "name", code: "invalid_length"},
		{name: "bad\x00name", timezone: "nowhere", field: "name", code: "invalid_characters"},
		{name: "Home", timezone: "Local", field: "timezone", code: "invalid_iana_timezone"},
		{name: "Home", timezone: "Nowhere/Nothing", field: "timezone", code: "invalid_iana_timezone"},
	} {
		_, err := validateHouseholdCreation(tc.name, tc.timezone)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != tc.field || validation.Code != tc.code {
			t.Fatalf("%q/%q: err=%v want %s/%s", tc.name, tc.timezone, err, tc.field, tc.code)
		}
	}
}
