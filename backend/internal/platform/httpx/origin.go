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

func WithRequestMetadata(r *http.Request, metadata RequestMetadata) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), metadataKey{}, metadata))
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
	metadata, peer := socketMetadata(r)
	if trustedPeer(peer, config.trusted) {
		if status := trustedForwardedMetadata(r, &metadata); status != 0 {
			return RequestMetadata{}, status
		}
	}
	if !canonicalAuthority(metadata, config.public) {
		return RequestMetadata{}, http.StatusMisdirectedRequest
	}
	if status := validateBrowserOrigin(r, config.public); status != 0 {
		return RequestMetadata{}, status
	}
	return metadata, 0
}

func socketMetadata(r *http.Request) (RequestMetadata, net.IP) {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	peer := net.ParseIP(remoteHost)
	metadata := RequestMetadata{Scheme: "http", Host: r.Host}
	if r.TLS != nil {
		metadata.Scheme = "https"
	}
	if peer != nil {
		metadata.ClientIP = peer.String()
	}
	if addr, err := netip.ParseAddr(remoteHost); err == nil && addr.Is6() && addr.Zone() == "" {
		metadata.ClientIP = addr.String()
	}
	return metadata, peer
}

func trustedPeer(peer net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if peer != nil && network.Contains(peer) {
			return true
		}
	}
	return false
}

func trustedForwardedMetadata(r *http.Request, metadata *RequestMetadata) int {
	forwarded, present, status := forwardedValues(r)
	if status != 0 {
		return status
	}
	if !present {
		x, xPresent, status := xForwardedValues(r)
		if status != 0 {
			return status
		}
		if xPresent {
			if x.scheme != "" {
				metadata.Scheme, metadata.Host = x.scheme, x.host
			}
			if x.clientIP != "" {
				metadata.ClientIP = x.clientIP
			}
		}
		return 0
	}
	x, xPresent, status := xForwardedValues(r)
	if status != 0 {
		return status
	}
	if xPresent && !forwardingAgrees(x, forwarded) {
		return http.StatusBadRequest
	}
	return applyForwardedMetadata(metadata, forwarded)
}

func applyForwardedMetadata(metadata *RequestMetadata, forwarded forwardedOrigin) int {
	metadata.Scheme, metadata.Host = forwarded.scheme, forwarded.host
	if forwarded.clientIP != "" {
		metadata.ClientIP = forwarded.clientIP
	}
	return 0
}

func forwardingAgrees(x xForwardedOrigin, forwarded forwardedOrigin) bool {
	return x.scheme == forwarded.scheme && sameAuthority(x.scheme, x.host, forwarded.host) &&
		(x.clientIP == "" || forwarded.clientIP == "" || x.clientIP == forwarded.clientIP)
}

func canonicalAuthority(metadata RequestMetadata, public parsedOrigin) bool {
	request, ok := parseAuthority(metadata.Scheme, metadata.Host)
	return ok && sameParsedOrigin(request, public)
}

func validateBrowserOrigin(r *http.Request, public parsedOrigin) int {
	origins := r.Header.Values("Origin")
	if len(origins) > 1 {
		return http.StatusForbidden
	}
	if len(origins) == 1 {
		origin, ok := parseOrigin(origins[0])
		if !ok || !sameParsedOrigin(origin, public) {
			return http.StatusForbidden
		}
		return 0
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return http.StatusForbidden
	}
	return 0
}

type forwardedOrigin struct{ scheme, host, clientIP string }

func forwardedValues(r *http.Request) (forwardedOrigin, bool, int) {
	value, present, status := singleForwardedEnvelope(r.Header.Values("Forwarded"))
	if status != 0 || !present {
		return forwardedOrigin{}, present, status
	}
	params, ok := decodeForwardedParams(value)
	if !ok {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	return validateForwardedParams(params)
}

func singleForwardedEnvelope(values []string) (string, bool, int) {
	if len(values) == 0 {
		return "", false, 0
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" || strings.Contains(values[0], ",") {
		return "", false, http.StatusBadRequest
	}
	return values[0], true, 0
}

func decodeForwardedParams(value string) (map[string]string, bool) {
	params := map[string]string{}
	for _, part := range strings.Split(value, ";") {
		key, raw, ok := strings.Cut(strings.TrimSpace(part), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		raw = strings.TrimSpace(raw)
		if !ok || !forwardedKey(key) || raw == "" {
			return nil, false
		}
		if _, exists := params[key]; exists {
			return nil, false
		}
		decoded, ok := forwardedValue(raw)
		if !ok {
			return nil, false
		}
		params[key] = decoded
	}
	return params, true
}

func forwardedKey(key string) bool {
	return key == "proto" || key == "host" || key == "for"
}

func validateForwardedParams(params map[string]string) (forwardedOrigin, bool, int) {
	proto, host := params["proto"], params["host"]
	if proto == "" || host == "" {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	if _, ok := parseAuthority(proto, host); !ok {
		return forwardedOrigin{}, false, http.StatusBadRequest
	}
	clientIP := ""
	if value := params["for"]; value != "" {
		var ok bool
		clientIP, ok = parseForwardedIP(value)
		if !ok {
			return forwardedOrigin{}, false, http.StatusBadRequest
		}
	}
	return forwardedOrigin{proto, host, clientIP}, true, 0
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
	proto, host, forwardedFor, present, status := xForwardedHeaders(r.Header)
	if status != 0 || !present {
		return xForwardedOrigin{}, present, status
	}
	return validateXForwarded(proto, host, forwardedFor)
}

func xForwardedHeaders(header http.Header) ([]string, []string, []string, bool, int) {
	proto := header.Values("X-Forwarded-Proto")
	host := header.Values("X-Forwarded-Host")
	forwardedFor := header.Values("X-Forwarded-For")
	if len(proto) > 1 || len(host) > 1 || len(forwardedFor) > 1 {
		return nil, nil, nil, false, http.StatusBadRequest
	}
	present := len(proto)+len(host)+len(forwardedFor) > 0
	if !present {
		return nil, nil, nil, false, 0
	}
	return proto, host, forwardedFor, true, 0
}

func validateXForwarded(proto, host, forwardedFor []string) (xForwardedOrigin, bool, int) {
	if len(forwardedFor) > 0 && (len(proto) != 1 || len(host) != 1) {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	if len(proto) == 0 && len(host) == 0 {
		ip, ok := forwardedClientIP(forwardedFor)
		if !ok {
			return xForwardedOrigin{}, false, http.StatusBadRequest
		}
		return xForwardedOrigin{clientIP: ip}, true, 0
	}
	if len(proto) != 1 || len(host) != 1 || strings.ContainsAny(proto[0]+host[0], ",\r\n\t ") || proto[0] != "http" && proto[0] != "https" {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	if _, ok := parseAuthority(proto[0], host[0]); !ok {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	ip, ok := forwardedClientIP(forwardedFor)
	if !ok {
		return xForwardedOrigin{}, false, http.StatusBadRequest
	}
	return xForwardedOrigin{proto[0], host[0], ip}, true, 0
}

func forwardedClientIP(values []string) (string, bool) {
	if len(values) == 0 {
		return "", true
	}
	if strings.Contains(values[0], ",") || strings.TrimSpace(values[0]) != values[0] {
		return "", false
	}
	ip := net.ParseIP(values[0])
	if ip == nil {
		return "", false
	}
	return ip.String(), true
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
