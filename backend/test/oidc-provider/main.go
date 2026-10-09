package main

import (
	"flag"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	listen := flag.String("listen", env("LISTEN_ADDR", ":8089"), "listen address")
	healthcheck := flag.Bool("healthcheck", false, "check local provider health and exit")
	issuer := flag.String("issuer", env("ISSUER_URL", ""), "issuer URL (discovery URL origin)")
	client := flag.String("client-id", env("CLIENT_ID", "test-client"), "client id")
	secret := flag.String("client-secret", env("CLIENT_SECRET", "test-secret"), "client secret")
	redirect := flag.String("redirect-uri", env("REDIRECT_URI", ""), "registered redirect URI")
	subjects := flag.String("subjects", env("SUBJECTS", "subject-1"), "comma-separated subjects")
	flag.Parse()
	if *healthcheck {
		resp, err := http.Get("http://127.0.0.1" + *listen + "/healthz")
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
