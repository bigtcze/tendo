package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Dynamic client registration policy (ADR 0023, RFC 7591, RFC 8252). The
// registration endpoint is unauthenticated, so every accepted field is checked
// here, before any database work.

var (
	// ErrMCPInvalidRedirectURI rejects a registration whose redirect URI fails
	// the redirect policy. The whole registration is refused.
	ErrMCPInvalidRedirectURI = errors.New("mcp invalid redirect uri")
	// ErrMCPInvalidClientMetadata rejects malformed or unacceptable RFC 7591
	// metadata.
	ErrMCPInvalidClientMetadata = errors.New("mcp invalid client metadata")
	// ErrMCPRegistrationFull refuses a registration while the grantless-client
	// cap is reached (ADR 0023).
	ErrMCPRegistrationFull = errors.New("mcp registration full")
)

const (
	// MCPMaxRedirectURIs is the redirect URI count bound of the client row.
	MCPMaxRedirectURIs = 5
	// MCPMaxRedirectURILength is the byte bound of one redirect URI.
	MCPMaxRedirectURILength = 2048
	// MCPMaxClientNameLength is the code point bound of client_name.
	MCPMaxClientNameLength = 100
	// MCPMaxRegistrationBytes bounds the registration request body.
	MCPMaxRegistrationBytes = 16 << 10
	// MCPDefaultClientName names a client that registers without one.
	MCPDefaultClientName = "MCP client"

	// MCPRegistrationCap bounds the clients that hold no grant.
	MCPRegistrationCap = 1000
)

// MCPGrantlessClientLifetime is how long a client without any grant or
// authorization code survives (ADR 0023).
const MCPGrantlessClientLifetime = 24 * time.Hour

// mcpLoopbackHosts are the literal hostnames an http redirect may name (RFC 8252
// §8.3). They are compared exactly: no alias, no trailing dot, no other case.
var mcpLoopbackHosts = map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true}

// mcpDeniedSchemes are private-use or browser-reachable schemes that must never
// be a redirect target.
var mcpDeniedSchemes = map[string]bool{
	"javascript": true, "vbscript": true, "data": true, "file": true, "blob": true,
	"about": true, "filesystem": true, "view-source": true, "ws": true, "wss": true,
	"ftp": true, "intent": true, "mailto": true, "tel": true, "sms": true, "jar": true,
	"res": true, "resource": true, "content": true, "shell": true, "search-ms": true,
	"android-app": true, "moz-extension": true, "safari-extension": true,
}

// mcpDeniedSchemePrefixes refuse whole families of browser-reachable schemes.
var mcpDeniedSchemePrefixes = []string{"chrome", "ms-"}

// mcpPrivateUseScheme is the lowercase scheme form RFC 3986 allows. The dot is
// not required: Cursor registers cursor://anysphere.cursor-mcp/... (ADR 0023
// records this deviation from the #456 review text).
var mcpPrivateUseScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]{1,63}$`)

// ValidateRedirectURI applies the redirect policy to one registered URI. The
// publicOrigin is Tendo's own canonical origin host; an https redirect to it is
// refused as a self-redirect.
func ValidateRedirectURI(raw, publicHost string) error {
	if raw == "" || len(raw) > MCPMaxRedirectURILength || !utf8.ValidString(raw) {
		return ErrMCPInvalidRedirectURI
	}
	if !rfc3986Only(raw) || strings.Contains(raw, "#") || strings.Contains(raw, "*") {
		return ErrMCPInvalidRedirectURI
	}
	u, err := url.Parse(raw)
	if err != nil || u.String() != raw || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return ErrMCPInvalidRedirectURI
	}
	switch u.Scheme {
	case "https":
		return validHTTPSRedirect(u, publicHost)
	case "http":
		return validHTTPRedirect(u)
	default:
		return validPrivateUseRedirect(u, u.Scheme)
	}
}

// rfc3986Only admits only RFC 3986 unreserved, reserved and percent characters.
func rfc3986Only(raw string) bool {
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("-._~:/?#[]@!$&'()+,;=%", c) >= 0:
		default:
			return false
		}
	}
	return true
}

func validHTTPSRedirect(u *url.URL, publicHost string) error {
	if u.Host == "" || u.Hostname() == "" {
		return ErrMCPInvalidRedirectURI
	}
	// A trailing dot names the same host (FQDN form), so it is stripped before
	// the self-redirect comparison: https://<publichost>./cb is refused too.
	if publicHost != "" && strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), publicHost) {
		return ErrMCPInvalidRedirectURI
	}
	return nil
}

// validHTTPRedirect accepts only the three exact loopback names with any port
// or none. The hostname is compared after the port is removed, so
// 127.0.0.1:8787 and [::1]:9000 pass while 127.1, localhost. and LOCALHOST do
// not.
func validHTTPRedirect(u *url.URL) error {
	if u.Host == "" || u.User != nil {
		return ErrMCPInvalidRedirectURI
	}
	host := u.Hostname()
	if !mcpLoopbackHosts[host] {
		return ErrMCPInvalidRedirectURI
	}
	// url.Hostname strips the brackets of an IPv6 literal; the literal must
	// have been written as [::1], not as a bare ::1 (which url rejects anyway).
	if host == "::1" && !strings.HasPrefix(u.Host, "[::1]") {
		return ErrMCPInvalidRedirectURI
	}
	return nil
}

// validPrivateUseRedirect accepts the lowercase private-use scheme in
// hierarchical form only, with no port. An opaque form such as scheme:foo is
// refused.
func validPrivateUseRedirect(u *url.URL, scheme string) error {
	if !mcpPrivateUseScheme.MatchString(scheme) || mcpDeniedSchemes[scheme] {
		return ErrMCPInvalidRedirectURI
	}
	for _, prefix := range mcpDeniedSchemePrefixes {
		if strings.HasPrefix(scheme, prefix) {
			return ErrMCPInvalidRedirectURI
		}
	}
	if u.Opaque != "" || u.Port() != "" {
		return ErrMCPInvalidRedirectURI
	}
	// "scheme://" requires a non-empty host; "scheme:/path" requires a path.
	if strings.HasPrefix(u.String(), scheme+"://") && u.Host == "" {
		return ErrMCPInvalidRedirectURI
	}
	if u.Host == "" && !strings.HasPrefix(u.Path, "/") {
		return ErrMCPInvalidRedirectURI
	}
	return nil
}

// ValidateRedirectURIs applies the count, duplicate and per-URI policy to a
// whole registration. Any invalid URI refuses the whole registration.
func ValidateRedirectURIs(uris []string, publicHost string) error {
	if len(uris) == 0 || len(uris) > MCPMaxRedirectURIs {
		return ErrMCPInvalidRedirectURI
	}
	seen := make(map[string]bool, len(uris))
	for _, raw := range uris {
		if err := ValidateRedirectURI(raw, publicHost); err != nil {
			return err
		}
		if seen[raw] {
			return ErrMCPInvalidRedirectURI
		}
		seen[raw] = true
	}
	return nil
}

// NormalizeClientName trims and NFC-normalizes a client_name and refuses control
// or format characters, which can disguise a consent prompt (Trojan Source,
// right-to-left override). An absent or empty name becomes MCPDefaultClientName.
func NormalizeClientName(raw *string) (string, error) {
	if raw == nil {
		return MCPDefaultClientName, nil
	}
	name := strings.TrimSpace(*raw)
	if name == "" {
		return MCPDefaultClientName, nil
	}
	if !utf8.ValidString(name) {
		return "", ErrMCPInvalidClientMetadata
	}
	for _, r := range name {
		// Zl and Zp are the line and paragraph separators (U+2028, U+2029):
		// they break a single-line consent prompt as control characters do.
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return "", ErrMCPInvalidClientMetadata
		}
	}
	name = nfcNormalize(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > MCPMaxClientNameLength {
		return "", ErrMCPInvalidClientMetadata
	}
	return name, nil
}

// nfcNormalize returns the NFC form of s, so a decomposed name is stored and
// measured in the same form a reader sees (decisions, "Client metadata").
func nfcNormalize(s string) string { return norm.NFC.String(s) }

// mcpClientMetadataKeys are the RFC 7591 request keys this server reads. Keys
// are matched exactly: encoding/json would match a struct tag case-insensitively,
// so REDIRECT_URIS would be taken as redirect_uris. Every other key is ignored.
const (
	mcpKeyClientName    = "client_name"
	mcpKeyRedirectURIs  = "redirect_uris"
	mcpKeyGrantTypes    = "grant_types"
	mcpKeyResponseTypes = "response_types"
)

// MCPClientRegistration is a decoded, not yet validated RFC 7591 request.
type MCPClientRegistration struct {
	Name          *string
	RedirectURIs  []string
	GrantTypes    []string
	ResponseTypes []string
}

// DecodeClientRegistration reads one RFC 7591 JSON object: valid UTF-8, a
// single object, no duplicate keys, at most MCPMaxRegistrationBytes, unknown
// keys ignored. Known fields with the wrong JSON type are refused.
func DecodeClientRegistration(body []byte) (MCPClientRegistration, error) {
	if len(body) > MCPMaxRegistrationBytes || !utf8.Valid(body) {
		return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return MCPClientRegistration{}, err
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&fields); err != nil {
		return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	reg := MCPClientRegistration{}
	if raw, ok := fields[mcpKeyClientName]; ok {
		var name string
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &name); err != nil {
				return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
			}
			reg.Name = &name
		}
	}
	raw, ok := fields[mcpKeyRedirectURIs]
	if !ok || string(raw) == "null" {
		return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	if err := json.Unmarshal(raw, &reg.RedirectURIs); err != nil {
		return MCPClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	if err := optionalStrings(fields[mcpKeyGrantTypes], &reg.GrantTypes); err != nil {
		return MCPClientRegistration{}, err
	}
	if err := optionalStrings(fields[mcpKeyResponseTypes], &reg.ResponseTypes); err != nil {
		return MCPClientRegistration{}, err
	}
	return reg, nil
}

// optionalStrings decodes an optional JSON string array. Absent or null is
// empty; any other type is invalid metadata.
func optionalStrings(raw json.RawMessage, dst *[]string) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return ErrMCPInvalidClientMetadata
	}
	return nil
}

// rejectDuplicateKeys refuses a JSON object that repeats a key at any depth.
func rejectDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := scanDuplicateKeys(dec); err != nil {
		return ErrMCPInvalidClientMetadata
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrMCPInvalidClientMetadata
	}
	return nil
}

func scanDuplicateKeys(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := keyTok.(string)
			if seen[key] {
				return ErrMCPInvalidClientMetadata
			}
			seen[key] = true
			if err := scanDuplicateKeys(dec); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case json.Delim('['):
		for dec.More() {
			if err := scanDuplicateKeys(dec); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	}
	return nil
}

// ValidatedClientRegistration is a registration that passed every policy
// check. Its grant and response types are fixed server policy, never copied
// from the request.
type ValidatedClientRegistration struct {
	Name         string
	RedirectURIs []string
}

// ValidateClientRegistration applies the metadata policy. publicHost is Tendo's
// own canonical host, used to refuse self-redirects.
func ValidateClientRegistration(reg MCPClientRegistration, publicHost string) (ValidatedClientRegistration, error) {
	if err := ValidateRedirectURIs(reg.RedirectURIs, publicHost); err != nil {
		return ValidatedClientRegistration{}, err
	}
	if len(reg.GrantTypes) > 0 && !containsString(reg.GrantTypes, "authorization_code") {
		return ValidatedClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	if len(reg.ResponseTypes) > 0 && !containsString(reg.ResponseTypes, "code") {
		return ValidatedClientRegistration{}, ErrMCPInvalidClientMetadata
	}
	name, err := NormalizeClientName(reg.Name)
	if err != nil {
		return ValidatedClientRegistration{}, err
	}
	return ValidatedClientRegistration{Name: name, RedirectURIs: append([]string(nil), reg.RedirectURIs...)}, nil
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// newMCPClientID mints a 32-byte base64url client identifier. It is a public
// identifier, so it is not hashed.
func newMCPClientID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("mcp client id generation failed")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
