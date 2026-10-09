package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/httporigin"
)

type Config struct {
	DatabaseURL       string
	ListenAddr        string
	PublicURL         string
	TrustedProxyCIDRs []string
	DBTimeout         time.Duration
	ShutdownTimeout   time.Duration
	SetupToken        string
	TestClockNow      *time.Time
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCDisplayName   string
}

func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	var c Config
	var ok bool
	c.DatabaseURL, ok = lookup("DATABASE_URL")
	if !ok || strings.TrimSpace(c.DatabaseURL) == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" || u.Host == "" || u.User == nil || u.User.Username() == "" || u.Path == "" || u.Path == "/" {
		return c, errors.New("DATABASE_URL must be a valid PostgreSQL URI with credentials and database")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return c, errors.New("DATABASE_URL must contain a valid query")
	}
	if len(query["sslmode"]) != 1 || len(query["ssl"]) != 0 {
		return c, errors.New("DATABASE_URL must set exactly one sslmode and must not use ssl")
	}
	sslmode := query["sslmode"][0]
	switch sslmode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return c, errors.New("DATABASE_URL must set a valid sslmode")
	}
	publicURL, ok := lookup("TENDO_PUBLIC_URL")
	if !ok || strings.TrimSpace(publicURL) == "" {
		return c, errors.New("TENDO_PUBLIC_URL is required")
	}
	canonical, err := canonicalOrigin(publicURL)
	if err != nil {
		return c, errors.New("TENDO_PUBLIC_URL must be an absolute HTTP(S) root origin")
	}
	c.PublicURL = canonical
	if err := loadOIDC(&c, lookup, canonical); err != nil {
		return Config{}, err
	}
	if proxies, ok := lookup("TENDO_TRUSTED_PROXY_CIDRS"); ok && strings.TrimSpace(proxies) != "" {
		for _, entry := range strings.Split(proxies, ",") {
			entry = strings.TrimSpace(entry)
			_, network, parseErr := net.ParseCIDR(entry)
			if parseErr != nil {
				return c, errors.New("invalid TENDO_TRUSTED_PROXY_CIDRS")
			}
			c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, network.String())
		}
	}
	clockNow, clockSet := lookup("TENDO_TEST_CLOCK_NOW")
	clockAck, ackSet := lookup("TENDO_TEST_CLOCK_ACK")
	if clockSet || ackSet {
		if !clockSet || strings.TrimSpace(clockNow) == "" {
			return c, errors.New("TENDO_TEST_CLOCK_NOW and TENDO_TEST_CLOCK_ACK must both be set and nonempty")
		}
		if !ackSet || strings.TrimSpace(clockAck) == "" {
			return c, errors.New("TENDO_TEST_CLOCK_NOW and TENDO_TEST_CLOCK_ACK must both be set and nonempty")
		}
		if clockAck != "isolated-e2e-only" {
			return c, errors.New("TENDO_TEST_CLOCK_ACK must equal isolated-e2e-only")
		}
		fixed, parseErr := time.Parse(time.RFC3339, clockNow)
		if parseErr != nil || fixed.Year() < 1 || fixed.Year() > 9999 || !strings.HasSuffix(clockNow, "Z") || fixed.UTC().Format(time.RFC3339) != clockNow {
			return c, errors.New("TENDO_TEST_CLOCK_NOW must be canonical whole-second UTC RFC3339 ending in Z")
		}
		public, _ := url.Parse(canonical)
		ip := net.ParseIP(public.Hostname())
		if public.Scheme != "http" || public.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return c, errors.New("TENDO_PUBLIC_URL must use HTTP and localhost or a literal loopback IP when the test clock is enabled")
		}
		if len(c.TrustedProxyCIDRs) != 0 {
			return c, errors.New("TENDO_TRUSTED_PROXY_CIDRS must be empty when the test clock is enabled")
		}
		c.TestClockNow = &fixed
	}
	c.ListenAddr, _ = lookup("TENDO_LISTEN_ADDR")
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	host, port, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return c, errors.New("invalid TENDO_LISTEN_ADDR")
	}
	if strings.TrimSpace(host) != host || port == "" {
		return c, errors.New("invalid TENDO_LISTEN_ADDR")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return c, errors.New("invalid TENDO_LISTEN_ADDR")
	}
	c.DBTimeout, err = duration(lookup, "TENDO_DB_TIMEOUT", 2*time.Second, 10*time.Second)
	if err != nil {
		return c, err
	}
	c.ShutdownTimeout, err = duration(lookup, "TENDO_SHUTDOWN_TIMEOUT", 10*time.Second, 60*time.Second)
	if err != nil {
		return c, err
	}
	c.SetupToken, _ = lookup("TENDO_SETUP_TOKEN")
	if c.SetupToken != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(c.SetupToken)
		if decodeErr != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != c.SetupToken {
			return Config{}, errors.New("TENDO_SETUP_TOKEN must be canonical standard base64 encoding of 32 bytes")
		}
	}
	return c, nil
}

func canonicalOrigin(raw string) (string, error) {
	origin, ok := httporigin.Parse(raw, true)
	if !ok {
		return "", errors.New("invalid origin")
	}
	return httporigin.Format(origin), nil
}

func duration(lookup func(string) (string, bool), key string, fallback, max time.Duration) (time.Duration, error) {
	s, ok := lookup(key)
	if !ok || s == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil { // permit plain integer seconds
		if n, nerr := strconv.ParseInt(s, 10, 64); nerr == nil && n > 0 && n <= int64(max/time.Second) {
			d = time.Duration(n) * time.Second
		} else {
			return 0, fmt.Errorf("invalid %s", key)
		}
	}
	if d <= 0 || d > max {
		return 0, fmt.Errorf("%s must be positive and no greater than %s", key, max)
	}
	return d, nil
}
