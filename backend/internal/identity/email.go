package identity

import "strings"

// maxEmailLength is the RFC 5321 forward-path limit. The database mirrors it
// structurally; this function is the only writer of the canonical form.
const maxEmailLength = 254

// CanonicalEmail is the single canonical form used for every stored and
// compared email address. It deliberately performs no Unicode normalization or
// IDNA conversion so that it needs no extra dependency: any non-ASCII byte makes
// the address unusable. Unusable input is reported with ok=false and callers
// treat it exactly like a missing address, never as a user-facing error.
//
// The local part stays case-sensitive and is never dot- or plus-tag-stripped:
// providers may treat those differently, and folding them would let two
// distinct mailboxes collide. Only the domain is lowercased.
func CanonicalEmail(raw string) (string, bool) {
	trimmed := strings.Trim(raw, " \t\r\n\v\f")
	if trimmed == "" || len(trimmed) > maxEmailLength {
		return "", false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < 0x21 || trimmed[i] > 0x7e {
			return "", false
		}
	}
	at := strings.IndexByte(trimmed, '@')
	if at <= 0 || at == len(trimmed)-1 || strings.IndexByte(trimmed[at+1:], '@') >= 0 {
		return "", false
	}
	local, domain := trimmed[:at], strings.ToLower(trimmed[at+1:])
	if !validEmailDomain(domain) {
		return "", false
	}
	return local + "@" + domain, true
}

// validEmailDomain accepts dotted LDH labels only. An A-label ("xn--...") is
// ASCII and therefore passes like any other label.
func validEmailDomain(domain string) bool {
	if domain == "" || strings.IndexByte(domain, '.') < 0 {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				continue
			}
			return false
		}
	}
	return true
}
