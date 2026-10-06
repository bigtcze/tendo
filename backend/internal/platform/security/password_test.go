package security

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	password := " p\u00e4ssword "
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("independent hashes must have distinct random salts")
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("unexpected PHC encoding: %q", first)
	}
	for _, candidate := range []string{password, "pässword"} {
		ok, err := VerifyPassword(first, candidate)
		if err != nil || (candidate == password && !ok) || (candidate != password && ok) {
			t.Fatalf("VerifyPassword(%q) = %v, %v", candidate, ok, err)
		}
	}
}

func TestVerifyPasswordRejectsMalformedOrUnsupportedHashes(t *testing.T) {
	valid, err := HashPassword("password")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid, "$")
	bad := []string{
		"", "not a hash", valid + "$extra",
		strings.Replace(valid, "argon2id", "argon2i", 1),
		strings.Replace(valid, "v=19", "v=16", 1),
		strings.Replace(valid, "m=65536", "m=4294967295", 1),
		strings.Replace(valid, "t=3", "t=4294967295", 1),
		strings.Replace(valid, "p=1", "p=0", 1),
		strings.Replace(valid, "m=65536", "m=65536,x=1", 1),
		strings.Replace(valid, parts[4], "%%", 1),
		strings.Replace(valid, parts[5], "AA", 1),
		strings.Replace(valid, parts[5], parts[5][:len(parts[5])-1], 1),
	}
	for _, encoded := range bad {
		ok, err := VerifyPassword(encoded, "password")
		if ok || err == nil || err.Error() != "invalid password hash" {
			t.Errorf("VerifyPassword rejected hash improperly: ok=%v err=%v hash=%q", ok, err, encoded)
		}
	}
	mutatedKey := parts[5]
	if mutatedKey[0] == 'A' {
		mutatedKey = "B" + mutatedKey[1:]
	} else {
		mutatedKey = "A" + mutatedKey[1:]
	}
	altered := mutatedKey
	tampered := strings.Replace(valid, parts[5], altered, 1)
	if ok, err := VerifyPassword(tampered, "password"); ok || err != nil {
		t.Fatalf("tampered key should be a mismatch, got %v, %v", ok, err)
	}
}

func TestPasswordLengthLimit(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("x", maxPasswordBytes)); err != nil {
		t.Fatalf("512-byte password rejected: %v", err)
	}
	if _, err := HashPassword(strings.Repeat("x", maxPasswordBytes+1)); err == nil {
		t.Fatal("expected oversized password rejection")
	}
	if ok, err := VerifyPassword("malformed", strings.Repeat("x", maxPasswordBytes+1)); ok || err == nil {
		t.Fatal("expected oversized password rejection before hash parsing")
	}
}
