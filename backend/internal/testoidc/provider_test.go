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

func TestTokenRequiresPKCEAndSingleUse(t *testing.T) {
	p, e := New()
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	redirect := p.Issuer() + "/callback"
	auth := func(verifier string) string {
		u, _ := url.Parse(p.Issuer() + "/authorize")
		q := u.Query()
		q.Set("client_id", p.ClientID)
		q.Set("response_type", "code")
		q.Set("code_challenge_method", "S256")
		sum := sha256.Sum256([]byte(verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("redirect_uri", redirect)
		q.Set("state", "s")
		q.Set("nonce", "n")
		u.RawQuery = q.Encode()
		client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Get(u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		return loc.Query().Get("code")
	}
	verifier := "correct-verifier-value"
	code := auth(verifier)
	post := func(verifier, red string) int {
		form := url.Values{"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {red}}
		r, err := http.Post(p.Issuer()+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		_, _ = io.ReadAll(r.Body)
		return r.StatusCode
	}
	if status := post("", redirect); status == 200 {
		t.Fatal("missing PKCE accepted")
	}
	code = auth(verifier)
	if status := post("wrong-verifier", redirect); status == 200 {
		t.Fatal("wrong PKCE accepted")
	}
	code = auth(verifier)
	if status := post(verifier, redirect); status != 200 {
		t.Fatalf("valid exchange status=%d", status)
	}
	if status := post(verifier, redirect); status == 200 {
		t.Fatal("reused code accepted")
	}
}
func TestTokenRedirectMismatch(t *testing.T) {
	p, e := New()
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	verifier := "correct-verifier"
	sum := sha256.Sum256([]byte(verifier))
	u, _ := url.Parse(p.Issuer() + "/authorize")
	q := u.Query()
	q.Set("client_id", p.ClientID)
	q.Set("response_type", "code")
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("redirect_uri", p.Issuer()+"/callback")
	q.Set("state", "s")
	q.Set("nonce", "n")
	u.RawQuery = q.Encode()
	r, e := http.Get(u.String())
	if e != nil {
		t.Fatal(e)
	}
	loc, _ := url.Parse(r.Header.Get("Location"))
	code := loc.Query().Get("code")
	r.Body.Close()
	f := url.Values{"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {p.Issuer() + "/wrong"}}
	resp, e := http.Post(p.Issuer()+"/token", "application/x-www-form-urlencoded", strings.NewReader(f.Encode()))
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("redirect mismatch accepted")
	}
}
