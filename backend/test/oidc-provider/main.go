package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func main() {
	listen := flag.String("listen", env("LISTEN_ADDR", ":8089"), "listen address")
	healthcheck := flag.Bool("healthcheck", false, "check local provider health and exit")
	tlsCert := flag.String("tls-cert", env("TLS_CERT_FILE", ""), "TLS certificate file")
	tlsKey := flag.String("tls-key", env("TLS_KEY_FILE", ""), "TLS private key file")
	issuer := flag.String("issuer", env("ISSUER_URL", ""), "issuer URL (discovery URL origin)")
	client := flag.String("client-id", env("CLIENT_ID", "test-client"), "client id")
	secret := flag.String("client-secret", env("CLIENT_SECRET", "test-secret"), "client secret")
	redirect := flag.String("redirect-uri", env("REDIRECT_URI", ""), "registered redirect URI")
	subjects := flag.String("subjects", env("SUBJECTS", "subject-1"), "comma-separated subjects")
	flag.Parse()
	if *healthcheck {
		scheme := "http"
		client := http.DefaultClient
		if *tlsCert != "" && *tlsKey != "" {
			scheme = "https"
			u, err := url.Parse(*issuer)
			if err != nil || u.Hostname() == "" {
				os.Exit(1)
			}
			tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
			if caFile := os.Getenv("SSL_CERT_FILE"); caFile != "" {
				pem, err := os.ReadFile(caFile)
				if err != nil {
					os.Exit(1)
				}
				roots, err := x509.SystemCertPool()
				if err != nil || roots == nil {
					roots = x509.NewCertPool()
				}
				if !roots.AppendCertsFromPEM(pem) {
					os.Exit(1)
				}
				tlsConfig.RootCAs = roots
			}
			client = &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
		}
		resp, err := client.Get(scheme + "://127.0.0.1" + *listen + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	p, err := testoidc.NewWithOptions("", *client, *secret, *redirect, split(*subjects))
	if err != nil {
		log.Fatal("provider init failed")
	}
	defer p.Close()
	if *issuer != "" {
		p.Server.URL = strings.TrimRight(*issuer, "/")
	}
	log.Printf("fake OIDC issuer=%s", p.Issuer())
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", p.Server.Config.Handler)
	if *tlsCert != "" && *tlsKey != "" {
		log.Fatal(http.ListenAndServeTLS(*listen, *tlsCert, *tlsKey, mux))
	}
	log.Fatal(http.ListenAndServe(*listen, mux))
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
