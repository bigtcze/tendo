package testoidc

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTokenPKCESingleUseAndRedirectBinding(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	redirect := p.Issuer() + "/callback"
	verifier := "correct-verifier-value"
	challenge := func(v string) string {
		sum := sha256.Sum256([]byte(v))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	authorize := func(red string) string {
		u, _ := url.Parse(p.Issuer() + "/authorize")
		q := u.Query()
		q.Set("client_id", p.ClientID)
		q.Set("response_type", "code")
		q.Set("code_challenge_method", "S256")
		q.Set("code_challenge", challenge(verifier))
		q.Set("redirect_uri", red)
		q.Set("state", "s")
		q.Set("nonce", "n")
		u.RawQuery = q.Encode()
		resp, e := noRedirectClient().Get(u.String())
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("authorize status=%d", resp.StatusCode)
		}
		loc, e := url.Parse(resp.Header.Get("Location"))
		if e != nil {
			t.Fatal(e)
		}
		code := loc.Query().Get("code")
		if code == "" {
			t.Fatalf("authorize redirect lacked code: %q", resp.Header.Get("Location"))
		}
		return code
	}
	post := func(code, verify, red string) int {
		form := url.Values{"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code": {code}, "code_verifier": {verify}, "redirect_uri": {red}}
		resp, e := http.Post(p.Issuer()+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		return resp.StatusCode
	}
	if got := post(authorize(redirect), "", redirect); got == http.StatusOK {
		t.Fatal("missing PKCE accepted")
	}
	if got := post(authorize(redirect), "wrong-verifier", redirect); got == http.StatusOK {
		t.Fatal("wrong PKCE accepted")
	}
	goodCode := authorize(redirect)
	if got := post(goodCode, verifier, redirect); got != http.StatusOK {
		t.Fatalf("valid exchange status=%d", got)
	}
	if got := post(goodCode, verifier, redirect); got == http.StatusOK {
		t.Fatal("reused code accepted")
	}
	mismatchCode := authorize(redirect)
	if got := post(mismatchCode, verifier, p.Issuer()+"/wrong"); got == http.StatusOK {
		t.Fatal("redirect mismatch accepted")
	}
}
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
