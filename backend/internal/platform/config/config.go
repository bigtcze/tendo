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

type envLookup func(string) (string, bool)

func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom validates the process environment fail-fast. The stage order is
// part of the operator contract: the first invalid setting in this order is
// the one reported. Errors never include configured values, so database
// credentials and secrets cannot leak into startup logs. On any error the
// returned Config is the zero value.
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	var c Config
	stages := []func(*Config, envLookup) error{
		loadDatabaseURL,
		loadPublicURL,
		func(c *Config, lookup envLookup) error { return loadOIDC(c, lookup, c.PublicURL) },
		loadTrustedProxies,
		loadTestClock,
		loadListenAddr,
		loadTimeouts,
		loadSetupToken,
	}
	for _, stage := range stages {
		if err := stage(&c, lookup); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

func loadDatabaseURL(c *Config, lookup envLookup) error {
	raw, ok := lookup("DATABASE_URL")
	if !ok || strings.TrimSpace(raw) == "" {
		return errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || !isPostgresURI(u) {
		return errors.New("DATABASE_URL must be a valid PostgreSQL URI with credentials and database")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("DATABASE_URL must contain a valid query")
	}
	if len(query["sslmode"]) != 1 || len(query["ssl"]) != 0 {
		return errors.New("DATABASE_URL must set exactly one sslmode and must not use ssl")
	}
	switch query["sslmode"][0] {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return errors.New("DATABASE_URL must set a valid sslmode")
	}
	c.DatabaseURL = raw
	return nil
}

func isPostgresURI(u *url.URL) bool {
	return (u.Scheme == "postgres" || u.Scheme == "postgresql") &&
		u.Host != "" && u.User != nil && u.User.Username() != "" &&
		u.Path != "" && u.Path != "/"
}

func loadPublicURL(c *Config, lookup envLookup) error {
	raw, ok := lookup("TENDO_PUBLIC_URL")
	if !ok || strings.TrimSpace(raw) == "" {
		return errors.New("TENDO_PUBLIC_URL is required")
	}
	canonical, err := canonicalOrigin(raw)
	if err != nil {
		return errors.New("TENDO_PUBLIC_URL must be an absolute HTTP(S) root origin")
	}
	c.PublicURL = canonical
	return nil
}

func loadTrustedProxies(c *Config, lookup envLookup) error {
	proxies, ok := lookup("TENDO_TRUSTED_PROXY_CIDRS")
	if !ok || strings.TrimSpace(proxies) == "" {
		return nil
	}
	for _, entry := range strings.Split(proxies, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(entry))
		if err != nil {
			return errors.New("invalid TENDO_TRUSTED_PROXY_CIDRS")
		}
		c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, network.String())
	}
	return nil
}

// loadTestClock accepts the reserved isolated-E2E business clock only as an
// explicit, acknowledged pair, and only for a loopback HTTP origin without
// trusted proxies, so it cannot be enabled on a reachable deployment.
func loadTestClock(c *Config, lookup envLookup) error {
	clockNow, clockSet := lookup("TENDO_TEST_CLOCK_NOW")
	clockAck, ackSet := lookup("TENDO_TEST_CLOCK_ACK")
	if !clockSet && !ackSet {
		return nil
	}
	if !clockSet || strings.TrimSpace(clockNow) == "" || !ackSet || strings.TrimSpace(clockAck) == "" {
		return errors.New("TENDO_TEST_CLOCK_NOW and TENDO_TEST_CLOCK_ACK must both be set and nonempty")
	}
	if clockAck != "isolated-e2e-only" {
		return errors.New("TENDO_TEST_CLOCK_ACK must equal isolated-e2e-only")
	}
	fixed, err := time.Parse(time.RFC3339, clockNow)
	if err != nil || fixed.Year() < 1 || fixed.Year() > 9999 || !strings.HasSuffix(clockNow, "Z") || fixed.UTC().Format(time.RFC3339) != clockNow {
		return errors.New("TENDO_TEST_CLOCK_NOW must be canonical whole-second UTC RFC3339 ending in Z")
	}
	if !isLoopbackHTTP(c.PublicURL) {
		return errors.New("TENDO_PUBLIC_URL must use HTTP and localhost or a literal loopback IP when the test clock is enabled")
	}
	if len(c.TrustedProxyCIDRs) != 0 {
		return errors.New("TENDO_TRUSTED_PROXY_CIDRS must be empty when the test clock is enabled")
	}
	c.TestClockNow = &fixed
	return nil
}

// isLoopbackHTTP reports whether a canonical public origin is plain HTTP on
// localhost or a literal loopback IP.
func isLoopbackHTTP(origin string) bool {
	public, _ := url.Parse(origin)
	if public.Scheme != "http" {
		return false
	}
	if public.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(public.Hostname())
	return ip != nil && ip.IsLoopback()
}

func loadListenAddr(c *Config, lookup envLookup) error {
	addr, _ := lookup("TENDO_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) != host || port == "" {
		return errors.New("invalid TENDO_LISTEN_ADDR")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("invalid TENDO_LISTEN_ADDR")
	}
	c.ListenAddr = addr
	return nil
}

func loadTimeouts(c *Config, lookup envLookup) error {
	var err error
	if c.DBTimeout, err = duration(lookup, "TENDO_DB_TIMEOUT", 2*time.Second, 10*time.Second); err != nil {
		return err
	}
	c.ShutdownTimeout, err = duration(lookup, "TENDO_SHUTDOWN_TIMEOUT", 10*time.Second, 60*time.Second)
	return err
}

func loadSetupToken(c *Config, lookup envLookup) error {
	token, _ := lookup("TENDO_SETUP_TOKEN")
	if token != "" {
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != token {
			return errors.New("TENDO_SETUP_TOKEN must be canonical standard base64 encoding of 32 bytes")
		}
	}
	c.SetupToken = token
	return nil
}

func canonicalOrigin(raw string) (string, error) {
	origin, ok := httporigin.Parse(raw, true)
	if !ok {
		return "", errors.New("invalid origin")
	}
	return httporigin.Format(origin), nil
}

func duration(lookup envLookup, key string, fallback, max time.Duration) (time.Duration, error) {
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
