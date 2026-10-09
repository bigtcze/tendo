package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

type envLookup func(string) (string, bool)

func loadOIDC(c *Config, lookup envLookup, public string) error {
	issuer, issuerSet := lookup("TENDO_OIDC_ISSUER")
	id, idSet := lookup("TENDO_OIDC_CLIENT_ID")
	secret, secretSet := lookup("TENDO_OIDC_CLIENT_SECRET")
	secretFile, fileSet := lookup("TENDO_OIDC_CLIENT_SECRET_FILE")
	display, displaySet := lookup("TENDO_OIDC_DISPLAY_NAME")
	enabled := issuerSet && issuer != ""
	idSet = idSet && id != ""
	secretSet = secretSet && secret != ""
	fileSet = fileSet && secretFile != ""
	displaySet = displaySet && (enabled || display != "")
	if !enabled {
		if idSet || secretSet || fileSet || displaySet {
			return errors.New("OIDC settings require TENDO_OIDC_ISSUER")
		}
		return nil
	}
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(issuer, u.Scheme+"://") {
		return errors.New("TENDO_OIDC_ISSUER must be an absolute HTTP(S) URL without userinfo, query, or fragment")
	}
	pub, _ := url.Parse(public)
	if pub.Scheme == "https" && u.Scheme != "https" || u.Scheme == "http" && pub.Scheme != "http" {
		return errors.New("TENDO_OIDC_ISSUER scheme is incompatible with TENDO_PUBLIC_URL")
	}
	if strings.TrimSpace(id) == "" || len(id) > 512 {
		return errors.New("TENDO_OIDC_CLIENT_ID is required and must be at most 512 bytes")
	}
	if secretSet && fileSet {
		return errors.New("set only one of TENDO_OIDC_CLIENT_SECRET and TENDO_OIDC_CLIENT_SECRET_FILE")
	}
	if fileSet {
		b, e := os.ReadFile(secretFile)
		if e != nil || len(b) == 0 {
			return errors.New("TENDO_OIDC_CLIENT_SECRET_FILE must be readable and nonempty")
		}
		secret = string(b)
	}
	if strings.TrimSpace(secret) == "" || len(secret) > 4096 {
		return errors.New("OIDC client secret is required and must be at most 4096 bytes")
	}
	if displaySet && (len(display) == 0 || len(display) > 80 || !utf8.ValidString(display) || strings.TrimSpace(display) == "" || strings.IndexFunc(display, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0) {
		return errors.New("TENDO_OIDC_DISPLAY_NAME must be printable and at most 80 bytes")
	}
	if !displaySet {
		display = "OpenID Connect"
	}
	c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret, c.OIDCDisplayName = issuer, id, secret, display
	return nil
}
