package httpx

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/httporigin"
)

type OriginPolicy struct {
	PublicURL         string
	TrustedProxyCIDRs []string
}

type RequestMetadata struct{ Scheme, Host, ClientIP string }
type metadataKey struct{}

type parsedOrigin struct {
	scheme string
	host   string
	port   string
}

type originConfig struct {
	public  parsedOrigin
	trusted []*net.IPNet
}

func MetadataFromRequest(r *http.Request) (RequestMetadata, bool) {
	m, ok := r.Context().Value(metadataKey{}).(RequestMetadata)
	return m, ok
}

func newOriginConfig(policy OriginPolicy) (originConfig, error) {
	public, ok := parseOrigin(policy.PublicURL)
	if !ok {
		return originConfig{}, http.ErrNotSupported
	}
	config := originConfig{public: public}
	for _, cidr := range policy.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return originConfig{}, err
		}
		config.trusted = append(config.trusted, network)
	}
	return config, nil
}

func originMiddlewareConfig(config originConfig, err error) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				writeProblem(w, http.StatusMisdirectedRequest, "Misdirected Request")
				return
			}
			metadata, status := requestOrigin(r, config)
			if status != 0 {
				title := "Bad Request"
				if status == http.StatusMisdirectedRequest {
					title = "Misdirected Request"
				} else if status == http.StatusForbidden {
					title = "Forbidden"
				}
				writeProblem(w, status, title)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), metadataKey{}, metadata)))
		})
	}
}

func requestOrigin(r *http.Request, config originConfig) (RequestMetadata, int) {
	remoteHost, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	if splitErr != nil {
		remoteHost = r.RemoteAddr
	}
	clientIP := net.ParseIP(remoteHost)
	metadata := RequestMetadata{Scheme: "http", Host: r.Host}
	if r.TLS != nil {
		metadata.Scheme = "https"
	}
	if clientIP != nil {
		metadata.ClientIP = clientIP.String()
	}
	if remoteAddr, err := netip.ParseAddr(remoteHost); err == nil && remoteAddr.Is6() && remoteAddr.Zone() == "" {
		metadata.ClientIP = remoteAddr.String()
	}
	trusted := false
	for _, network := range config.trusted {
		if clientIP != nil && network.Contains(clientIP) {
			trusted = true
			break
		}
	}
	if trusted {
		forwarded, present, status := forwardedValues(r)
		if status != 0 {
			return RequestMetadata{}, status
		}
		if present {
			x, xPresent, status := xForwardedValues(r)
			if status != 0 {
				return RequestMetadata{}, status
			}
			if xPresent && (x.scheme != forwarded.scheme || !sameAuthority(x.scheme, x.host, forwarded.host) || x.clientIP != "" && forwarded.clientIP != "" && x.clientIP != forwarded.clientIP) {
				return RequestMetadata{}, http.StatusBadRequest
			}
			metadata.Scheme, metadata.Host = forwarded.scheme, forwarded.host
			if forwarded.clientIP != "" {
				metadata.ClientIP = forwarded.clientIP
			}
		} else {
			x, supplied, status := xForwardedValues(r)
			if status != 0 {
				return RequestMetadata{}, status
			}
			if supplied {
				if x.scheme != "" {
					metadata.Scheme, metadata.Host = x.scheme, x.host
				}
				if x.clientIP != "" {
					metadata.ClientIP = x.clientIP
				}
			}
		}
	}
	request, ok := parseAuthority(metadata.Scheme, metadata.Host)
	if !ok || !sameParsedOrigin(request, config.public) {
		return RequestMetadata{}, http.StatusMisdirectedRequest
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 {
		return RequestMetadata{}, http.StatusForbidden
	}
	if len(origins) == 1 {
		origin, ok := parseOrigin(origins[0])
		if !ok || !sameParsedOrigin(origin, config.public) {
			return RequestMetadata{}, http.StatusForbidden
		}
	} else if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return RequestMetadata{}, http.StatusForbidden
	}

	return metadata, 0
}

type forwardedOrigin struct{ scheme, host, clientIP string }

func forwardedValues(r *http.Request) (forwardedOrigin, bool, int) {
	values := r.Header.Values("Forwarded")
	if len(values) == 0 {
		return forwardedOrigin{}, false, 0
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" || strings.Contains(values[0], ",") {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	params := map[string]string{}
	for _, part := range strings.Split(values[0], ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !ok || (key != "proto" && key != "host" && key != "for") || key == "" || value == "" {
			return forwardedOrigin{}, false, http.StatusBadRequest
		}
		if _, exists := params[key]; exists {
			return forwardedOrigin{}, false, http.StatusBadRequest
		}
		decoded, ok := forwardedValue(value)
		if !ok {
			return forwardedOrigin{}, false, http.StatusBadRequest
		}
		params[key] = decoded
	}
	if params["proto"] == "" || params["host"] == "" {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	if _, ok := parseAuthority(params["proto"], params["host"]); !ok {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	clientIP := ""
	if value := params["for"]; value != "" {
		clientIP, _ = parseForwardedIP(value)
		if clientIP == "" {
			return forwardedOrigin{}, false, http.StatusBadRequest
		}
	}
	return forwardedOrigin{params["proto"], params["host"], clientIP}, true, 0
}

func forwardedValue(value string) (string, bool) {
	if !strings.HasPrefix(value, "\"") && !strings.HasSuffix(value, "\"") {
		return value, !strings.ContainsAny(value, "\"\\ \t")
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' || strings.ContainsAny(value[1:len(value)-1], "\"\\\r\n") {
		return "", false
	}
	return value[1 : len(value)-1], true
}

func parseForwardedIP(value string) (string, bool) {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), true
	}
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		ip := net.ParseIP(host)
		n, parseErr := strconv.Atoi(port)
		if ip != nil && parseErr == nil && n > 0 && n <= 65535 {
			return ip.String(), true
		}
	}
	return "", false
}

type xForwardedOrigin struct{ scheme, host, clientIP string }

func xForwardedValues(r *http.Request) (xForwardedOrigin, bool, int) {
	proto, host, forwardedFor := r.Header.Values("X-Forwarded-Proto"), r.Header.Values("X-Forwarded-Host"), r.Header.Values("X-Forwarded-For")
	if len(proto) > 1 || len(host) > 1 || len(forwardedFor) > 1 {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	if len(proto) == 0 && len(host) == 0 && len(forwardedFor) == 0 {
		return xForwardedOrigin{}, false, 0
	}
	if len(forwardedFor) > 0 && (len(proto) != 1 || len(host) != 1) {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	if len(proto) == 0 && len(host) == 0 {
		clientIP := ""
		if len(forwardedFor) == 1 {
			if strings.Contains(forwardedFor[0], ",") || strings.TrimSpace(forwardedFor[0]) != forwardedFor[0] {
				return xForwardedOrigin{}, false, http.StatusBadRequest
			}
			ip := net.ParseIP(forwardedFor[0])
			if ip == nil {
				return xForwardedOrigin{}, false, http.StatusBadRequest
			}
			clientIP = ip.String()
		}
		return xForwardedOrigin{clientIP: clientIP}, true, 0
	}
	if len(proto) != 1 || len(host) != 1 || strings.ContainsAny(proto[0]+host[0], ",\r\n\t ") || proto[0] != "http" && proto[0] != "https" {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	if len(proto) == 1 {
		if _, ok := parseAuthority(proto[0], host[0]); !ok {
			return xForwardedOrigin{}, false, http.StatusBadRequest
		}
	}
	clientIP := ""
	if len(forwardedFor) == 1 {
		if strings.Contains(forwardedFor[0], ",") || strings.TrimSpace(forwardedFor[0]) != forwardedFor[0] {
			return xForwardedOrigin{}, false, http.StatusBadRequest
		}
		ip := net.ParseIP(forwardedFor[0])
		if ip == nil {
			return xForwardedOrigin{}, false, http.StatusBadRequest
		}
		clientIP = ip.String()
	}
	return xForwardedOrigin{proto[0], host[0], clientIP}, true, 0
}

func parseOrigin(raw string) (parsedOrigin, bool) {
	origin, ok := httporigin.Parse(raw, false)
	if !ok {
		return parsedOrigin{}, false
	}
	return parsedOrigin{scheme: origin.Scheme, host: origin.Host, port: origin.Port}, true
}

func parseAuthority(scheme, authority string) (parsedOrigin, bool) {
	origin, ok := httporigin.ParseAuthority(scheme, authority)
	if !ok {
		return parsedOrigin{}, false
	}
	return parsedOrigin{scheme: origin.Scheme, host: origin.Host, port: origin.Port}, true
}

func sameParsedOrigin(a, b parsedOrigin) bool {
	return a.scheme == b.scheme && a.host == b.host && a.port == b.port
}

func sameAuthority(scheme, a, b string) bool {
	left, leftOK := parseAuthority(scheme, a)
	right, rightOK := parseAuthority(scheme, b)
	return leftOK && rightOK && sameParsedOrigin(left, right)
}
