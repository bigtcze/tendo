package httpx

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestOriginCharacterization(t *testing.T) {
	configFor := func(cidr string) originConfig {
		t.Helper()
		config, err := newOriginConfig(OriginPolicy{PublicURL: "https://example.test", TrustedProxyCIDRs: []string{cidr}})
		if err != nil {
			t.Fatal(err)
		}
		return config
	}
	cases := []struct {
		name, method, host, remote string
		tls                        bool
		headers                    http.Header
		cidr                       string
		wantStatus                 int
		want                       RequestMetadata
	}{
		{name: "IPv6 expanded and compressed equivalence", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=\"[2001:0db8:0:0:0:0:0:1]\";proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"2001:db8::1"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "2001:db8::1"}},
		{name: "IPv6 no forwarding trusted peer", method: http.MethodGet, host: "internal", remote: "[2001:db8::2]:1", tls: true, cidr: "2001:db8::/126", wantStatus: http.StatusMisdirectedRequest},
		{name: "trusted canonical socket tls", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", tls: true, cidr: "192.0.2.0/24", want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "192.0.2.4"}},
		{name: "IPv6 trusted inside", method: http.MethodGet, host: "internal", remote: "[2001:db8::1]:1", cidr: "2001:db8::/126", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "2001:db8::1"}},
		{name: "forwarded malformed backslash", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {`proto="https\";host=example.test`}}, wantStatus: http.StatusBadRequest},
		{name: "IPv6 trusted outside", method: http.MethodGet, host: "internal", remote: "[2001:db8::4]:1", cidr: "2001:db8::/126", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, wantStatus: http.StatusMisdirectedRequest},
		{name: "trusted no forwarding wrong socket scheme", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", wantStatus: http.StatusMisdirectedRequest},
		{name: "malformed forwarded precedes foreign origin", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"bad"}, "Origin": {"https://foreign.test"}}, wantStatus: http.StatusBadRequest},
		{name: "wrong authority precedes foreign origin", method: http.MethodGet, host: "wrong.test", remote: "203.0.113.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Origin": {"https://foreign.test"}}, wantStatus: http.StatusMisdirectedRequest},
		{name: "post origin required", method: http.MethodPost, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", wantStatus: http.StatusForbidden},
		{name: "GET missing origin", method: http.MethodGet, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "203.0.113.4"}},
		{name: "HEAD missing origin", method: http.MethodHead, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "203.0.113.4"}},
		{name: "OPTIONS missing origin", method: http.MethodOptions, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "203.0.113.4"}},
		{name: "PUT missing origin", method: http.MethodPut, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", wantStatus: http.StatusForbidden},
		{name: "PATCH missing origin", method: http.MethodPatch, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", wantStatus: http.StatusForbidden},
		{name: "DELETE missing origin", method: http.MethodDelete, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", wantStatus: http.StatusForbidden},
		{name: "get foreign origin", method: http.MethodGet, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", headers: http.Header{"Origin": {"https://foreign.test"}}, wantStatus: http.StatusForbidden},
		{name: "duplicate origin", method: http.MethodGet, host: "example.test", remote: "203.0.113.4:1", tls: true, cidr: "192.0.2.0/24", headers: http.Header{"Origin": {"https://example.test", "https://example.test"}}, wantStatus: http.StatusForbidden},
		{name: "duplicate XFP", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Proto": {"https", "https"}}, wantStatus: http.StatusBadRequest},
		{name: "duplicate XFH", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Host": {"example.test", "example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "duplicate XFF", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-For": {"203.0.113.1", "203.0.113.1"}}, wantStatus: http.StatusBadRequest},
		{name: "XFP only", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Proto": {"https"}}, wantStatus: http.StatusBadRequest},
		{name: "XFH only", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Host": {"example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "XFF only rejected", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", tls: true, cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-For": {"203.0.113.8"}}, wantStatus: http.StatusBadRequest},
		{name: "empty XFP", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Proto": {""}, "X-Forwarded-Host": {"example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "empty XFH", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {""}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded key case fold", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"Proto=https;HOST=example.test"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "192.0.2.4"}},
		{name: "forwarded duplicate key", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;PROTO=https;host=example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded backslash quoted", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {`proto="https\\";host=example.test`}}, wantStatus: http.StatusBadRequest},
		{name: "quoted values", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=\"https\";host=\"example.test\""}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "192.0.2.4"}},
		{name: "forwarded trailing semicolon", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test;"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded empty element", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;;host=example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded by unsupported", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test;by=x"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded uppercase proto rejected", method: http.MethodGet, host: "example.test", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=HTTPS;host=example.test"}}, wantStatus: http.StatusBadRequest},
		{name: "Forwarded and X disagreement with client IP", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=203.0.113.1;proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"203.0.113.2"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded host disagreement", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"other.test"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded client IP disagreement", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=203.0.113.1;proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"203.0.113.2"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded port disagreement", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test:444"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded no for retains socket", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "192.0.2.4"}},
		{name: "Forwarded without for retains socket IP despite XFF", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"203.0.113.8"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "192.0.2.4"}},
		{name: "forwarded matching port 444", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test:444"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test:444"}}, wantStatus: http.StatusMisdirectedRequest},
		{name: "forwarded and X port disagree 444", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=example.test:443"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test:444"}}, wantStatus: http.StatusBadRequest},
		{name: "forwarded with for and X omitted", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=203.0.113.9;proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "203.0.113.9"}},
		{name: "IPv6 client IP equivalent", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=\"[2001:0db8:0:0:0:0:0:1]\";proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"2001:db8::1"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "2001:db8::1"}},
		{name: "forwarded host preserves accepted spelling", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"proto=https;host=EXAMPLE.test:00443"}}, want: RequestMetadata{Scheme: "https", Host: "EXAMPLE.test:00443", ClientIP: "192.0.2.4"}},
		{name: "forwarded for honored", method: http.MethodGet, host: "internal", remote: "192.0.2.4:1", cidr: "192.0.2.0/24", headers: http.Header{"Forwarded": {"for=203.0.113.8;proto=https;host=example.test"}}, want: RequestMetadata{Scheme: "https", Host: "example.test", ClientIP: "203.0.113.8"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://placeholder/", nil)
			r.Host, r.RemoteAddr = tc.host, tc.remote
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			for key, values := range tc.headers {
				for _, value := range values {
					r.Header.Add(key, value)
				}
			}
			got, status := requestOrigin(r, configFor(tc.cidr))
			wantStatus := tc.wantStatus
			if status != wantStatus || got != tc.want {
				t.Fatalf("metadata=%+v status=%d, want %+v status=%d", got, status, tc.want, wantStatus)
			}
		})
	}
}
