package httpx

import "net/http"

// ValidIdempotencyKey reports whether key is a usable Idempotency-Key value:
// 1 to 128 bytes of visible ASCII (0x21-0x7e), compared case-sensitively and
// never normalized. Domain services that persist keys keep their own copy of
// the rule because they must not import the transport layer; this is the one
// transport-side spelling shared by the HTTP adapters.
func ValidIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '!' || key[i] > '~' {
			return false
		}
	}
	return true
}

// IdempotencyKey reads the single required Idempotency-Key header. A missing
// header is 400 idempotency_key_required; a repeated header or an invalid value
// (including an empty one) is 400 invalid_idempotency_key. On failure it has
// already written the problem and returns false.
func IdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) == 0 {
		ProblemResponse(w, http.StatusBadRequest, "idempotency_key_required")
		return "", false
	}
	if len(keys) != 1 || !ValidIdempotencyKey(keys[0]) {
		ProblemResponse(w, http.StatusBadRequest, "invalid_idempotency_key")
		return "", false
	}
	return keys[0], true
}
