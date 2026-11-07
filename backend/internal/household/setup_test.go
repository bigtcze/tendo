package household

import (
	"errors"
	"testing"
)

func TestValidateHouseholdName(t *testing.T) {
	for _, tc := range []struct {
		name, input, want, code string
	}{
		{"czech diacritics", "Rodina Novákových", "Rodina Novákových", ""},
		{"trimmed", "  Veselí  ", "Veselí", ""},
		{"zero width space only", "\u200b", "", "invalid_characters"},
		{"byte order mark only", "\ufeff", "", "invalid_characters"},
		{"embedded zero width space", "Rodina\u200bNovák", "", "invalid_characters"},
		{"bidi override", "Home\u202e", "", "invalid_characters"},
		{"embedded line separator", "a\u2028b", "", "invalid_characters"},
		{"embedded paragraph separator", "a\u2029b", "", "invalid_characters"},
		{"line separator only", "\u2028", "", "invalid_length"},
		{"paragraph separator only", "\u2029", "", "invalid_length"},
		{"no-break space only", "\u00a0", "", "invalid_length"},
		{"newline", "bad\nname", "", "invalid_characters"},
		{"invalid utf8", "\xff", "", "invalid_characters"},
		{"empty", "", "", "invalid_length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Validate(tc.input, "Europe/Prague")
			if tc.code == "" {
				if err != nil || got != tc.want {
					t.Fatalf("Validate(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
				}
				return
			}
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != "householdName" || validation.Code != tc.code {
				t.Fatalf("Validate(%q) error = %v; want householdName %s", tc.input, err, tc.code)
			}
		})
	}
}
