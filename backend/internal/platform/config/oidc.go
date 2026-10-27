package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const defaultOIDCDisplayName = "OpenID Connect"

// oidcSettings are the raw OIDC variables. An empty value counts as unset,
// except that TENDO_OIDC_DISPLAY_NAME set to empty is rejected once OIDC is
// enabled.
type oidcSettings struct {
	issuer, clientID, secret, secretFile, display string
	displaySet                                    bool
}

func readOIDCSettings(lookup envLookup) oidcSettings {
	var s oidcSettings
	s.issuer, _ = lookup("TENDO_OIDC_ISSUER")
	s.clientID, _ = lookup("TENDO_OIDC_CLIENT_ID")
	s.secret, _ = lookup("TENDO_OIDC_CLIENT_SECRET")
	s.secretFile, _ = lookup("TENDO_OIDC_CLIENT_SECRET_FILE")
	s.display, s.displaySet = lookup("TENDO_OIDC_DISPLAY_NAME")
	return s
}

// loadOIDC validates the optional single OIDC provider. It is disabled when
// the issuer is empty, in which case no other OIDC setting may be present.
func loadOIDC(c *Config, lookup envLookup, public string) error {
	s := readOIDCSettings(lookup)
	if s.issuer == "" {
		if s.clientID != "" || s.secret != "" || s.secretFile != "" || s.display != "" {
			return errors.New("OIDC settings require TENDO_OIDC_ISSUER")
		}
		return nil
	}
	if err := validateIssuer(s.issuer, public); err != nil {
		return err
	}
	if strings.TrimSpace(s.clientID) == "" || len(s.clientID) > 512 {
		return errors.New("TENDO_OIDC_CLIENT_ID is required and must be at most 512 bytes")
	}
	secret, err := clientSecret(s)
	if err != nil {
		return err
	}
	display, err := displayName(s)
	if err != nil {
		return err
	}
	c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret, c.OIDCDisplayName = s.issuer, s.clientID, secret, display
	return nil
}

func validateIssuer(issuer, public string) error {
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(issuer, u.Scheme+"://") {
		return errors.New("TENDO_OIDC_ISSUER must be an absolute HTTP(S) URL without userinfo, query, or fragment")
	}
	// A plain-HTTP provider is accepted only for a plain-HTTP (local
	// development) origin; an HTTPS deployment always needs an HTTPS provider.
	pub, _ := url.Parse(public)
	if u.Scheme == "http" && pub.Scheme != "http" {
		return errors.New("TENDO_OIDC_ISSUER scheme is incompatible with TENDO_PUBLIC_URL")
	}
	return nil
}

func clientSecret(s oidcSettings) (string, error) {
	if s.secret != "" && s.secretFile != "" {
		return "", errors.New("set only one of TENDO_OIDC_CLIENT_SECRET and TENDO_OIDC_CLIENT_SECRET_FILE")
	}
	secret := s.secret
	if s.secretFile != "" {
		b, err := os.ReadFile(s.secretFile)
		if err != nil || len(b) == 0 {
			return "", errors.New("TENDO_OIDC_CLIENT_SECRET_FILE must be readable and nonempty")
		}
		secret = string(b)
	}
	if strings.TrimSpace(secret) == "" || len(secret) > 4096 {
		return "", errors.New("OIDC client secret is required and must be at most 4096 bytes")
	}
	return secret, nil
}

func displayName(s oidcSettings) (string, error) {
	if !s.displaySet {
		return defaultOIDCDisplayName, nil
	}
	d := s.display
	if len(d) > 80 || !utf8.ValidString(d) || strings.TrimSpace(d) == "" || strings.IndexFunc(d, notPrintable) >= 0 {
		return "", errors.New("TENDO_OIDC_DISPLAY_NAME must be printable and at most 80 bytes")
	}
	return d, nil
}

func notPrintable(r rune) bool { return !unicode.IsPrint(r) }
