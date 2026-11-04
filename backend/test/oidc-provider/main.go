package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"

	"github.com/bigtcze/tendo/backend/internal/testoidc"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type options struct {
	listen, tlsCert, tlsKey, issuer, client, secret, redirect, subjects string
	healthcheck                                                         bool
}

func parseOptions() options {
	var opts options
	flag.StringVar(&opts.listen, "listen", env("LISTEN_ADDR", ":8089"), "listen address")
	flag.BoolVar(&opts.healthcheck, "healthcheck", false, "check local provider health and exit")
	flag.StringVar(&opts.tlsCert, "tls-cert", env("TLS_CERT_FILE", ""), "TLS certificate file")
	flag.StringVar(&opts.tlsKey, "tls-key", env("TLS_KEY_FILE", ""), "TLS private key file")
	flag.StringVar(&opts.issuer, "issuer", env("ISSUER_URL", ""), "issuer URL (discovery URL origin)")
	flag.StringVar(&opts.client, "client-id", env("CLIENT_ID", "test-client"), "client id")
	flag.StringVar(&opts.secret, "client-secret", env("CLIENT_SECRET", "test-secret"), "client secret")
	flag.StringVar(&opts.redirect, "redirect-uri", env("REDIRECT_URI", ""), "registered redirect URI")
	flag.StringVar(&opts.subjects, "subjects", env("SUBJECTS", "subject-1"), "comma-separated subjects")
	flag.Parse()
	return opts
}

func main() {
	opts := parseOptions()
	tlsEnabled := opts.tlsCert != "" || opts.tlsKey != ""
	if (opts.tlsCert == "") != (opts.tlsKey == "") {
		if opts.healthcheck {
			os.Exit(1)
		}
		log.Fatal("both TLS certificate and key must be set")
	}
	if opts.healthcheck {
		if err := checkHealth(opts.listen, opts.issuer, tlsEnabled); err != nil {
			os.Exit(1)
		}
		return
	}
	serveProvider(opts, tlsEnabled)
}

func checkHealth(listen, issuer string, tlsEnabled bool) error {
	client := http.DefaultClient
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
		u, err := url.Parse(issuer)
		if err != nil || u.Hostname() == "" {
			return fmt.Errorf("invalid issuer")
		}
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
		if caFile := os.Getenv("SSL_CERT_FILE"); caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				return err
			}
			roots, err := x509.SystemCertPool()
			if err != nil || roots == nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("invalid CA certificate bundle")
			}
			tlsConfig.RootCAs = roots
		}
		client = &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	}
	resp, err := client.Get(scheme + "://127.0.0.1" + listen + "/healthz")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	return nil
}

func serveProvider(opts options, tlsEnabled bool) {
	p, err := testoidc.NewWithOptions("", opts.client, opts.secret, opts.redirect, split(opts.subjects))
	if err != nil {
		log.Fatal("provider init failed")
	}
	defer p.Close()
	if opts.issuer != "" {
		p.Server.URL = strings.TrimRight(opts.issuer, "/")
	}
	log.Printf("fake OIDC issuer=%s", p.Issuer())
	mux := providerHandler(p)
	if tlsEnabled {
		log.Fatal(http.ListenAndServeTLS(opts.listen, opts.tlsCert, opts.tlsKey, mux))
	}
	log.Fatal(http.ListenAndServe(opts.listen, mux))
}

func providerHandler(p *testoidc.Provider) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", p.Server.Config.Handler)
	return mux
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func split(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
