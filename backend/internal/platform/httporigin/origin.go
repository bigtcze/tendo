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
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Host == "" || u.Path != "" && !(allowRootSlash && u.Path == "/") {
		return Origin{}, false
	}
	origin, ok := ParseAuthority(u.Scheme, u.Host)
	if !ok {
		return Origin{}, false
	}
	return origin, true
}

func ParseAuthority(scheme, authority string) (Origin, bool) {
	if scheme != "http" && scheme != "https" || authority == "" || strings.ContainsAny(authority, "@/?#\\%\r\n\t ") {
		return Origin{}, false
	}
	var host, port string
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return Origin{}, false
		}
		addr, err := netip.ParseAddr(authority[1:end])
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return Origin{}, false
		}
		host = addr.String()
		rest := authority[end+1:]
		if rest != "" {
			if !strings.HasPrefix(rest, ":") || strings.Contains(rest[1:], ":") {
				return Origin{}, false
			}
			port = rest[1:]
		}
	} else {
		if strings.Contains(authority, "[") || strings.Contains(authority, "]") || strings.Count(authority, ":") > 1 {
			return Origin{}, false
		}
		value := strings.ToLower(authority)
		if strings.Contains(value, ":") {
			host, port, _ = strings.Cut(value, ":")
		} else {
			host = value
		}
		if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
			return Origin{}, false
		}
		for _, r := range host {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
				return Origin{}, false
			}
		}
	}
	if strings.HasSuffix(authority, ":") {
		return Origin{}, false
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return Origin{}, false
		}
		port = strconv.Itoa(n)
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
