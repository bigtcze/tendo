package subject

import "testing"

func TestValidUUIDCharacterization(t *testing.T) {
	cases := []struct {
		name, value string
		want        bool
	}{
		{"canonical lowercase", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70", true}, {"uppercase", "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B70", true}, {"mixed case", "0198A2f0-7c1E-7a53-9B0e-5d3F2c1a4B70", true},
		{"wrong hyphen positions", "0198a2f-07c1e-7a53-9b0e-5d3f2c1a4b70", false}, {"missing hyphens", "0198a2f07c1e7a539b0e5d3f2c1a4b70", false}, {"braces", "{0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70}", false}, {"URN prefix", "urn:uuid:0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70", false},
		{"Unicode digit", "٠198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70", false}, {"full-width characters", "０198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70", false}, {"non-hex letters", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bg0", false}, {"empty", "", false},
		{"too short", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b7", false}, {"too long", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b700", false}, {"trailing whitespace", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70 ", false}, {"trailing newline", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validUUID(tc.value); got != tc.want {
				t.Fatalf("validUUID(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
