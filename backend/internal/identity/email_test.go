package identity

import (
	"math/rand"
	"strings"
	"testing"
)

func TestCanonicalEmailAcceptedForms(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "anna@example.test", "anna@example.test"},
		{"surrounding ascii whitespace", " \tanna@example.test\r\n", "anna@example.test"},
		{"domain lowercased", "Anna@Example.TEST", "Anna@example.test"},
		{"case-sensitive local part kept", "ANNA@example.test", "ANNA@example.test"},
		{"plus tag kept", "anna+tag@example.test", "anna+tag@example.test"},
		{"dots kept", "an.na@example.test", "an.na@example.test"},
		{"idna a-label allowed", "anna@xn--mnchen-3ya.example", "anna@xn--mnchen-3ya.example"},
		{"hyphen inside label", "a@my-domain.example", "a@my-domain.example"},
		{"single character local", "a@b.co", "a@b.co"},
		{"max length", strings.Repeat("a", maxEmailLength-len("@b.co")) + "@b.co", strings.Repeat("a", maxEmailLength-len("@b.co")) + "@b.co"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CanonicalEmail(tc.in)
			if !ok || got != tc.want {
				t.Fatalf("CanonicalEmail(%q)=%q,%v want %q", tc.in, got, ok, tc.want)
			}
		})
	}
}

func TestCanonicalEmailRejectedForms(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"empty", ""},
		{"whitespace only", " \t\r\n"},
		{"missing at", "anna.example.test"},
		{"empty local", "@example.test"},
		{"empty domain", "anna@"},
		{"two at signs", "anna@example@test"},
		{"internal space", "an na@example.test"},
		{"non ascii local", "änna@example.test"},
		{"non ascii domain", "anna@exämple.test"},
		{"unicode whitespace around", "\u00a0anna@example.test"},
		{"control character", "anna\x00@example.test"},
		{"domain without dot", "anna@example"},
		{"empty label", "anna@example..test"},
		{"leading hyphen label", "anna@-example.test"},
		{"trailing hyphen label", "anna@example-.test"},
		{"underscore label", "anna@exa_mple.test"},
		{"too long", strings.Repeat("a", maxEmailLength) + "@b.co"},
		{"label too long", "anna@" + strings.Repeat("a", 64) + ".test"},
		{"display name form", "Anna <anna@example.test>"},
		{"trailing dot", "anna@example.test."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := CanonicalEmail(tc.in); ok {
				t.Fatalf("CanonicalEmail(%q)=%q accepted", tc.in, got)
			}
		})
	}
}

// TestCanonicalEmailProperties checks invariants over a deterministic corpus
// instead of a fuzz-only target, so the property always runs in normal CI.
func TestCanonicalEmailProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(20261110))
	alphabet := []byte("abAB09.-_+@ \t\x00\x7f\u00e4")
	for i := 0; i < 4000; i++ {
		var sb strings.Builder
		for n := rng.Intn(24); n >= 0; n-- {
			sb.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		input := sb.String()
		canonical, ok := CanonicalEmail(input)
		if !ok {
			continue
		}
		if len(canonical) > maxEmailLength {
			t.Fatalf("canonical form too long for %q", input)
		}
		if canonical != strings.Trim(canonical, " \t\r\n\v\f") {
			t.Fatalf("canonical form has outer whitespace: %q", canonical)
		}
		again, okAgain := CanonicalEmail(canonical)
		if !okAgain || again != canonical {
			t.Fatalf("CanonicalEmail is not idempotent: %q -> %q ok=%v", canonical, again, okAgain)
		}
		local, domain, found := strings.Cut(canonical, "@")
		if !found || local == "" || domain != strings.ToLower(domain) {
			t.Fatalf("canonical form violates structure: %q", canonical)
		}
	}
}

func FuzzCanonicalEmail(f *testing.F) {
	for _, seed := range []string{
		"", "anna@example.test", "Anna@Example.TEST", "@x.test", "a@b",
		"änna@example.test", "anna@exämple.test", "a@-b.test", strings.Repeat("a", 300) + "@b.co",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		canonical, ok := CanonicalEmail(raw)
		if !ok {
			return
		}
		if len(canonical) > maxEmailLength {
			t.Fatalf("canonical form too long")
		}
		again, okAgain := CanonicalEmail(canonical)
		if !okAgain || again != canonical {
			t.Fatalf("not idempotent: %q -> %q", canonical, again)
		}
	})
}
