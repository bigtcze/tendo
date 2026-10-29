package httporigin

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type Origin struct {
	Scheme string
	Host   string
	Port   string
}

func Parse(raw string, allowRootSlash bool) (Origin, bool) {
	if raw == "" || strings.Contains(raw, "#") {
		return Origin{}, false
	}
	u, err := url.Parse(raw)
	if invalidParsedOrigin(u, err, allowRootSlash) {
		return Origin{}, false
	}
	origin, ok := ParseAuthority(u.Scheme, u.Host)
	if !ok {
		return Origin{}, false
	}
	return origin, true
}

func invalidParsedOrigin(u *url.URL, err error, allowRootSlash bool) bool {
	return err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Host == "" ||
		u.Path != "" && !(allowRootSlash && u.Path == "/")
}

func ParseAuthority(scheme, authority string) (Origin, bool) {
	if scheme != "http" && scheme != "https" || authority == "" || invalidAuthorityCharacters(authority) {
		return Origin{}, false
	}
	host, port, ok := parseAuthorityHost(authority)
	if !ok {
		return Origin{}, false
	}
	port, ok = canonicalPort(port)
	if !ok {
		return Origin{}, false
	}
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return Origin{Scheme: scheme, Host: host, Port: port}, true
}

func invalidAuthorityCharacters(authority string) bool {
	return strings.HasSuffix(authority, ":") || strings.ContainsAny(authority, "@/?#\\%\r\n\t ")
}

func parseAuthorityHost(authority string) (string, string, bool) {
	if strings.HasPrefix(authority, "[") {
		return parseBracketedIPv6(authority)
	}
	return parseDNSAuthority(authority)
}

func parseBracketedIPv6(authority string) (string, string, bool) {
	end := strings.IndexByte(authority, ']')
	if end < 0 {
		return "", "", false
	}
	addr, err := netip.ParseAddr(authority[1:end])
	if err != nil || !addr.Is6() || addr.Zone() != "" {
		return "", "", false
	}
	rest := authority[end+1:]
	if rest == "" {
		return addr.String(), "", true
	}
	if !strings.HasPrefix(rest, ":") || strings.Contains(rest[1:], ":") {
		return "", "", false
	}
	return addr.String(), rest[1:], true
}

func parseDNSAuthority(authority string) (string, string, bool) {
	if !isASCIIAuthority(authority) || invalidDNSDelimiters(authority) {
		return "", "", false
	}
	value := asciiLower(authority)
	host, port, hasPort := strings.Cut(value, ":")
	if !hasPort {
		host = value
	}
	if !validDNSHost(host) {
		return "", "", false
	}
	return host, port, true
}

func isASCIIAuthority(authority string) bool {
	for i := 0; i < len(authority); i++ {
		if authority[i] >= 0x80 {
			return false
		}
	}
	return true
}

func invalidDNSDelimiters(authority string) bool {
	return strings.Contains(authority, "[") || strings.Contains(authority, "]") ||
		strings.Count(authority, ":") > 1 || strings.HasSuffix(authority, ":")
}

func validDNSHost(host string) bool {
	if !validDNSShape(host) {
		return false
	}
	for i := 0; i < len(host); i++ {
		if !isDNSByte(host[i]) {
			return false
		}
	}
	return true
}

func validDNSShape(host string) bool {
	return host != "" && !strings.HasPrefix(host, ".") &&
		!strings.HasSuffix(host, ".") && !strings.Contains(host, "..")
}

func isDNSByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-'
}

func asciiLower(value string) string {
	b := []byte(value)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func canonicalPort(port string) (string, bool) {
	if port == "" {
		return "", true
	}
	for i := 0; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return "", false
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	return strconv.Itoa(n), true
}

func Format(origin Origin) string {
	port := origin.Port
	if origin.Scheme == "http" && port == "80" || origin.Scheme == "https" && port == "443" {
		port = ""
	}
	host := origin.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return origin.Scheme + "://" + host
}
