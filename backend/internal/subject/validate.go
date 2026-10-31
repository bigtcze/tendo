package subject

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseType accepts only the exact lowercase wire values.
func ParseType(s string) (Type, bool) {
	switch t := Type(s); t {
	case TypePerson, TypeHome, TypeVehicle, TypePet, TypeCustom:
		return t, true
	}
	return "", false
}

// ValidateName returns the trimmed name or a ValidationError.
func ValidateName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", &ValidationError{"name", "invalid_characters"}
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", &ValidationError{"name", "invalid_length"}
	}
	for _, r := range name {
		// Control, format (zero-width, bidi), and line or paragraph separator
		// characters can hide or reorder text.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return "", &ValidationError{"name", "invalid_characters"}
		}
	}
	return name, nil
}
