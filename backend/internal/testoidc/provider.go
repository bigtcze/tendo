package testoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Faults struct {
	Deny, BadSignature, WrongIssuer, WrongAudience, WrongAZP, MultiAudienceNoAZP, Expired, FutureIssuedAt, NonceMismatch, MissingIDToken, TokenServerError bool
	Delay                                                                                                                                                  time.Duration
	MissingAuthEndpoint, MissingTokenEndpoint, MissingJWKSURI, WrongJWKSHTTP, SecureAuthEndpoints, NoCodeResponseType                                      bool
	DiscoveryUnavailable, InvalidGrant                                                                                                                     bool
	SubjectEmpty, SubjectNonASCII, SubjectTooLong, MissingIssuedAt, StringIssuedAt, FractionalIssuedAt, DisallowedAlgorithm                                bool
}
type Provider struct {
	Server                                       *httptest.Server
	mu                                           sync.Mutex
	key, other                                   *rsa.PrivateKey
	codes                                        map[string]code
	ClientID, ClientSecret, Subject, RedirectURI string
	Subjects                                     []string
	Faults                                       Faults
}
type code struct {
	challenge, redirect, nonce, subject string
	expires                             time.Time
	used                                bool
}

func New() (*Provider, error) {
	return NewWithOptions("", "test-client", "test-secret", "", []string{"subject-1"})
}
func NewWithOptions(issuer, client, secret, redirect string, subjects []string) (*Provider, error) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return nil, e
	}
	other, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return nil, e
	}
	p := &Provider{key: key, other: other, codes: map[string]code{}, ClientID: client, ClientSecret: secret, RedirectURI: redirect, Subjects: subjects}
	if len(subjects) > 0 {
		p.Subject = subjects[0]
	}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	if issuer != "" {
		p.Server.URL = issuer
	}
	return p, nil
}
func (p *Provider) Close()         { p.Server.Close() }
func (p *Provider) Issuer() string { return p.Server.URL }
func (p *Provider) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		if p.Faults.DiscoveryUnavailable {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		auth, token, jwks := p.Issuer()+"/authorize", p.Issuer()+"/token", p.Issuer()+"/keys"
		if p.Faults.SecureAuthEndpoints {
			auth, token, jwks = "https://auth.test/authorize", "https://auth.test/token", "https://auth.test/keys"
		}
		if p.Faults.MissingAuthEndpoint {
			auth = ""
		}
		if p.Faults.MissingTokenEndpoint {
			token = ""
		}
		if p.Faults.MissingJWKSURI {
			jwks = ""
		}
		if p.Faults.WrongJWKSHTTP {
			jwks = "http://127.0.0.1:1/keys"
		}
		responses := []string{"code"}
		if p.Faults.NoCodeResponseType {
			responses = []string{"id_token"}
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": p.Issuer(), "authorization_endpoint": auth, "token_endpoint": token, "jwks_uri": jwks, "response_types_supported": responses, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"}})
	case "/keys":
		p.keys(w)
	case "/authorize":
		p.authorize(w, r)
	case "/token":
		p.token(w, r)
	default:
		http.NotFound(w, r)
	}
}
func (p *Provider) keys(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	n := p.key.PublicKey.N
	e := big.NewInt(int64(p.key.PublicKey.E))
	json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(n.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e.Bytes())}}})
}
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || redirect == "" || p.RedirectURI != "" && redirect != p.RedirectURI {
		http.Error(w, "invalid request", 400)
		return
	}
	if p.Faults.Deny || q.Get("deny") == "1" {
		u, _ := url.Parse(redirect)
		v := u.Query()
		v.Set("error", "access_denied")
		v.Set("state", q.Get("state"))
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), 302)
		return
	}
	sub := q.Get("sub")
	if sub == "" {
		sub = q.Get("login_hint")
	}
	if sub == "" {
		sub = p.Subject
	}
	valid := false
	for _, x := range p.Subjects {
		if x == sub {
			valid = true
		}
	}
	if len(p.Subjects) == 0 && sub == p.Subject {
		valid = true
	}
	if !valid {
		http.Error(w, "unknown subject", 400)
		return
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	codeval := base64.RawURLEncoding.EncodeToString(b)
	p.mu.Lock()
	p.codes[codeval] = code{challenge: q.Get("code_challenge"), redirect: redirect, nonce: q.Get("nonce"), subject: sub, expires: time.Now().Add(2 * time.Minute)}
	p.mu.Unlock()
	u, _ := url.Parse(redirect)
	v := u.Query()
	v.Set("code", codeval)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), 302)
}
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if p.Faults.Delay > 0 {
		time.Sleep(p.Faults.Delay)
	}
	if p.Faults.TokenServerError {
		http.Error(w, "upstream", 500)
		return
	}
	if p.Faults.InvalidGrant {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	_ = r.ParseForm()
	basicUser, basicSecret, hasBasic := r.BasicAuth()
	if !((r.Form.Get("client_id") == p.ClientID && r.Form.Get("client_secret") == p.ClientSecret) || (hasBasic && basicUser == p.ClientID && basicSecret == p.ClientSecret)) {
		http.Error(w, "client", 401)
		return
	}
	p.mu.Lock()
	c, ok := p.codes[r.Form.Get("code")]
	if ok && (!c.used) && time.Now().Before(c.expires) {
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("redirect_uri") != c.redirect || r.Form.Get("code_verifier") == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
			p.mu.Unlock()
			http.Error(w, "pkce or redirect", 400)
			return
		}
		c.used = true
		p.codes[r.Form.Get("code")] = c
	} else {
		ok = false
	}
	p.mu.Unlock()
	if !ok {
		http.Error(w, "invalid code", 400)
		return
	}
	tok, e := p.idToken(c)
	if e != nil {
		http.Error(w, "signing", 500)
		return
	}
	res := map[string]any{"access_token": "discard", "token_type": "Bearer", "expires_in": 300}
	if !p.Faults.MissingIDToken {
		res["id_token"] = tok
	}
	json.NewEncoder(w).Encode(res)
}
func (p *Provider) idToken(c code) (string, error) {
	now := time.Now()
	iss := p.Issuer()
	var aud any = p.ClientID
	claims := map[string]any{"iss": iss, "sub": c.subject, "aud": aud, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(), "nonce": c.nonce}
	if p.Faults.SubjectEmpty {
		claims["sub"] = ""
	}
	if p.Faults.SubjectNonASCII {
		claims["sub"] = "sübject"
	}
	if p.Faults.SubjectTooLong {
		claims["sub"] = strings.Repeat("s", 256)
	}
	if p.Faults.MissingIssuedAt {
		delete(claims, "iat")
	}
	if p.Faults.StringIssuedAt {
		claims["iat"] = "not-numeric"
	}
	if p.Faults.FractionalIssuedAt {
		claims["iat"] = float64(now.Unix()) + 0.5
	}
	if p.Faults.WrongIssuer {
		claims["iss"] = "https://wrong.test"
	}
	if p.Faults.WrongAudience {
		claims["aud"] = "wrong-client"
	}
	if p.Faults.WrongAZP {
		claims["aud"] = []string{p.ClientID, "second"}
		claims["azp"] = "wrong-client"
	}
	if p.Faults.MultiAudienceNoAZP {
		claims["aud"] = []string{p.ClientID, "second"}
	}
	if p.Faults.Expired {
		claims["exp"] = now.Add(-time.Minute).Unix()
	}
	if p.Faults.FutureIssuedAt {
		claims["iat"] = now.Add(2 * time.Minute).Unix()
	}
	if p.Faults.NonceMismatch {
		claims["nonce"] = "wrong"
	}
	alg := "RS256"
	if p.Faults.DisallowedAlgorithm {
		alg = "HS256"
	}
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": "test", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	msg := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	key := p.key
	if p.Faults.BadSignature {
		key = p.other
	}
	sum := sha256.Sum256([]byte(msg))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if e != nil {
		return "", e
	}
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
