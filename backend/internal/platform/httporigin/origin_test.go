package httporigin

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		allowSlash        bool
		ok                bool
	}{
		{name: "expanded IPv6 origin", input: "https://[2001:0DB8:0:0:0:0:0:1]:443", want: "https://[2001:db8::1]", allowSlash: true, ok: true},
		{name: "expanded IPv6 browser origin", input: "https://[2001:0db8:0:0:0:0:0:1]:443", want: "https://[2001:db8::1]", allowSlash: false, ok: true},
		{name: "uppercase DNS browser origin", input: "https://EXAMPLE.test:443", want: "https://example.test", allowSlash: false, ok: true},
		{name: "browser origin is strict", input: "https://example.test/", ok: false},
		{name: "config root slash", input: "https://example.test/", want: "https://example.test", allowSlash: true, ok: true},
		{name: "empty query", input: "https://example.test?", allowSlash: true},
		{name: "empty fragment", input: "https://example.test#", allowSlash: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Parse(tc.input, tc.allowSlash)
			if ok != tc.ok || ok && Format(got) != tc.want {
				t.Fatalf("Parse(%q)=(%+v,%v)", tc.input, got, ok)
			}
		})
	}
}

func TestIsASCIIAuthority(t *testing.T) {
	if isASCIIAuthority("iİ.example") {
		t.Fatal("Unicode authority was classified as ASCII")
	}
	if !isASCIIAuthority("EXAMPLE.test") {
		t.Fatal("ASCII authority was rejected")
	}
}

func TestCanonicalPortRequiresDigitsAndNormalizes(t *testing.T) {
	if _, ok := canonicalPort("+443"); ok {
		t.Fatal("signed port was accepted")
	}
	if got, ok := canonicalPort("00443"); !ok || got != "443" {
		t.Fatalf("canonicalPort(00443)=(%q,%v)", got, ok)
	}
}

func TestAsciiLower(t *testing.T) {
	if got := asciiLower("EXAMPLE.test"); got != "example.test" {
		t.Fatalf("asciiLower returned %q", got)
	}
}

func TestParseAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, scheme, input, want string
		ok                        bool
	}{
		{name: "compressed IPv6", scheme: "https", input: "[2001:0DB8:0:0:0:0:0:1]", want: "https://[2001:db8::1]", ok: true},
		{name: "mapped IPv6 stays IPv6", scheme: "https", input: "[::ffff:192.0.2.1]", want: "https://[::ffff:192.0.2.1]", ok: true},
		{name: "default HTTP port", scheme: "http", input: "EXAMPLE.test:80", want: "http://example.test", ok: true},
		{name: "unbracketed IPv6", scheme: "http", input: "2001:db8::1:80"},
		{name: "unbracketed IPv6 with port", scheme: "http", input: "2001:db8::1:8080"},
		{name: "IPv4 mapped IPv6 canonical form", scheme: "https", input: "[::ffff:192.0.2.1]", want: "https://[::ffff:192.0.2.1]", ok: true},
		{name: "bracketed DNS", scheme: "http", input: "[example.test]"},
		{name: "zone", scheme: "http", input: "[fe80::1%eth0]"},
		{name: "invalid port", scheme: "https", input: "example.test:65536"},
		{name: "zero port", scheme: "https", input: "example.test:0"},
		{name: "maximum port", scheme: "https", input: "example.test:65535", want: "https://example.test:65535", ok: true},
		{name: "leading zero port", scheme: "https", input: "EXAMPLE.test:00443", want: "https://example.test", ok: true},
		{name: "empty explicit port", scheme: "https", input: "example.test:"},
		{name: "bracket suffix", scheme: "https", input: "[::1]junk"},
		{name: "bracket multiple port", scheme: "https", input: "[::1]:443:1"},
		{name: "bracketed IPv4", scheme: "https", input: "[127.0.0.1]"},
		{name: "unclosed bracket", scheme: "https", input: "[::1"},
		{name: "double dot", scheme: "https", input: "a..b"},
		{name: "leading dot", scheme: "https", input: ".a"},
		{name: "trailing dot", scheme: "https", input: "a."},
		{name: "empty authority", scheme: "https", input: ""},
		{name: "unicode byte after ASCII prefix", scheme: "https", input: "iK.example"},
		{name: "Kelvin byte after ASCII prefix", scheme: "https", input: "i\u212A.example"},
		{name: "ASCII prefix plus Unicode dotted I", scheme: "https", input: "iİ.example"},
		{name: "punycode host", scheme: "https", input: "xn--bcher-kva.example", want: "https://xn--bcher-kva.example", ok: true},
		{name: "signed positive port", scheme: "https", input: "example.test:+443"},
		{name: "signed negative zero port", scheme: "https", input: "example.test:-0"},
		{name: "unicode dotted I", scheme: "https", input: "İ.example"},
		{name: "unicode Kelvin sign", scheme: "https", input: "\u212A.example"},
		{name: "unicode long s", scheme: "https", input: "ſ.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseAuthority(tc.scheme, tc.input)
			if ok != tc.ok || ok && Format(got) != tc.want {
				t.Fatalf("ParseAuthority(%q,%q)=(%+v,%v)", tc.scheme, tc.input, got, ok)
			}
		})
	}
}
