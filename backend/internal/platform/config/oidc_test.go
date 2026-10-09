package config

import (
	"os"
	"strings"
	"testing"
)

func TestOIDCConfig(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://user:dbsecret@localhost:5432/tendo?sslmode=disable", "TENDO_PUBLIC_URL": "https://tendo.test"}
	valid := map[string]string{"TENDO_OIDC_ISSUER": "https://issuer.test", "TENDO_OIDC_CLIENT_ID": "client", "TENDO_OIDC_CLIENT_SECRET": "secret-value"}
	cases := []struct {
		name string
		set  map[string]string
		bad  bool
	}{
		{"disabled", nil, false}, {"partial", map[string]string{"TENDO_OIDC_CLIENT_ID": "x"}, true},
		{"issuer scheme", merge(valid, map[string]string{"TENDO_OIDC_ISSUER": "ftp://issuer.test"}), true}, {"userinfo", merge(valid, map[string]string{"TENDO_OIDC_ISSUER": "https://user:pass@issuer.test"}), true}, {"query", merge(valid, map[string]string{"TENDO_OIDC_ISSUER": "https://issuer.test?x=y"}), true}, {"fragment", merge(valid, map[string]string{"TENDO_OIDC_ISSUER": "https://issuer.test#x"}), true}, {"https public http issuer", merge(valid, map[string]string{"TENDO_OIDC_ISSUER": "http://issuer.test"}), true}, {"secret both", merge(valid, map[string]string{"TENDO_OIDC_CLIENT_SECRET_FILE": "unused"}), true}, {"empty display", merge(valid, map[string]string{"TENDO_OIDC_DISPLAY_NAME": ""}), true}, {"display bounds", merge(valid, map[string]string{"TENDO_OIDC_DISPLAY_NAME": strings.Repeat("a", 81)}), true}, {"display controls", merge(valid, map[string]string{"TENDO_OIDC_DISPLAY_NAME": "bad\nname"}), true}, {"client id bounds", merge(valid, map[string]string{"TENDO_OIDC_CLIENT_ID": strings.Repeat("x", 513)}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := merge(base, tc.set)
			if tc.name == "empty secret file" {
				f := t.TempDir() + "/empty"
				if e := os.WriteFile(f, nil, 0600); e != nil {
					t.Fatal(e)
				}
				values["TENDO_OIDC_CLIENT_SECRET_FILE"] = f
			}
			_, err := LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
			if tc.bad && err == nil {
				t.Fatal("expected invalid config")
			}
			if !tc.bad && err != nil {
				t.Fatal(err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "dbsecret") || strings.Contains(err.Error(), "pass@")) {
				t.Fatalf("secret leaked: %v", err)
			}
		})
	}
	f := t.TempDir() + "/empty"
	if err := os.WriteFile(f, nil, 0600); err != nil {
		t.Fatal(err)
	}
	values := merge(base, valid)
	delete(values, "TENDO_OIDC_CLIENT_SECRET")
	values["TENDO_OIDC_CLIENT_SECRET_FILE"] = f
	if _, err := LoadFrom(func(k string) (string, bool) { v, ok := values[k]; return v, ok }); err == nil {
		t.Fatal("expected empty secret file rejected")
	}
}
func merge(a, b map[string]string) map[string]string {
	r := map[string]string{}
	for k, v := range a {
		r[k] = v
	}
	for k, v := range b {
		r[k] = v
	}
	return r
}
