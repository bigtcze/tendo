package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL     string
	ListenAddr      string
	DBTimeout       time.Duration
	ShutdownTimeout time.Duration
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
	return c, nil
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
