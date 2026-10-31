#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -z "${PROXY:-}" && -z "${PROXY_SMOKE_CHILD:-}" ]]; then
  for selected_proxy in caddy nginx traefik; do
    printf '\n=== Reverse proxy smoke: %s ===\n' "$selected_proxy"
    PROXY="$selected_proxy" PROXY_SMOKE_CHILD=1 bash "$0"
    printf 'PASS: %s\n' "$selected_proxy"
  done
  exit 0
fi
setup_environment() {
  project="tendo-proxy-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
  proxy=${PROXY:-caddy}
  case "$proxy" in
    caddy|nginx|traefik) ;;
    *) printf 'Unknown proxy %s; use caddy, nginx, or traefik\n' "$proxy" >&2; exit 2 ;;
  esac
  temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-proxy-smoke.XXXXXX")
  chmod 700 "$temp_dir"
  cert_dir="$temp_dir/certs"
  smoke_dir="$temp_dir/smoke"
  mkdir "$cert_dir" "$smoke_dir"
  chmod 755 "$cert_dir" "$smoke_dir"
  export COMPOSE_PROJECT_NAME=$project
  export POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_OIDC_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_OIDC_ISSUER TENDO_OIDC_CLIENT_ID TENDO_OIDC_CLIENT_SECRET TENDO_OIDC_CLIENT_SECRET_FILE TENDO_OIDC_DISPLAY_NAME
  POSTGRES_PASSWORD=$(openssl rand -hex 32)
  TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
  TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
  TENDO_OIDC_ISSUER=
  TENDO_OIDC_CLIENT_ID=
  TENDO_OIDC_CLIENT_SECRET=
  TENDO_OIDC_CLIENT_SECRET_FILE=
  TENDO_OIDC_DISPLAY_NAME=
  TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  TENDO_OIDC_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  TENDO_OIDC_CLIENT_SECRET=tendo-proxy-oidc-secret
  secret_inventory="$temp_dir/oidc-secret-values"
  : >"$secret_inventory"
  chmod 600 "$secret_inventory"
  record_secret() {
    [[ -n "$1" ]] || return 0
    (umask 077; printf '%s\n' "$1" >>"$secret_inventory")
    chmod 600 "$secret_inventory"
  }
  record_secret "$TENDO_OIDC_CLIENT_SECRET"
  TENDO_RESTART_POLICY=no
  TENDO_DB_TIMEOUT=2
  TENDO_SHUTDOWN_TIMEOUT=10
  TENDO_PUBLIC_URL=https://tendo.test
  TENDO_TRUSTED_PROXY_CIDRS=
  compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml" -f "$temp_dir/compose.yaml")
  migrate=(docker compose --profile migration --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml" -f "$temp_dir/compose.yaml")
}

cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    if "${compose[@]}" logs --no-color >"$temp_dir/logs" 2>&1; then
      if python3 - "$temp_dir/logs" "$temp_dir/redacted-logs" <<'PY'
import os, re, sys
text = open(sys.argv[1], encoding="utf-8").read()
for name in ("POSTGRES_PASSWORD", "TENDO_DATABASE_PASSWORD", "TENDO_SETUP_TOKEN", "TENDO_OIDC_CLIENT_SECRET"):
    value = os.environ.get(name, "")
    if value:
        text = text.replace(value, "[REDACTED]")
try:
    with open(os.path.join(os.path.dirname(sys.argv[1]), "oidc-secret-values"), encoding="utf-8") as inventory:
        for secret in inventory:
            secret = secret.rstrip("\n")
            if secret:
                text = text.replace(secret, "[REDACTED]")
except FileNotFoundError:
    pass
text = re.sub(r"(?i)(POSTGRES_PASSWORD|TENDO_DATABASE_PASSWORD|TENDO_SETUP_TOKEN|TENDO_OIDC_CLIENT_SECRET)(=|%3[dD])[^\s&]+", r"\1\2[REDACTED]", text)
with open(sys.argv[2], "w", encoding="utf-8") as redacted:
    redacted.write(text)
PY
      then
        if ! cat "$temp_dir/redacted-logs" >&2; then
          printf 'Failure log redaction failed; suppressing logs\n' >&2
        fi
      else
        printf 'Failure log redaction failed; suppressing logs\n' >&2
      fi
    else
      printf 'Could not capture failure logs; suppressing logs\n' >&2
    fi
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    printf 'Compose cleanup failed for project %s\n' "$project" >&2
    (( result != 0 )) || result=1
  fi
  if ! rm -rf -- "$temp_dir"; then
    printf 'Could not remove smoke temporary directory %s\n' "$temp_dir" >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
setup_certificates_and_compose() {
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$temp_dir/ca.key" -out "$cert_dir/ca.crt" -subj '/CN=Tendo proxy smoke CA' -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
  chmod 644 "$cert_dir/ca.crt"
  openssl req -newkey rsa:2048 -nodes -keyout "$cert_dir/tendo.test.key" -out "$cert_dir/tendo.test.csr" -subj '/CN=tendo.test' >/dev/null 2>&1
  printf 'subjectAltName=DNS:tendo.test\nextendedKeyUsage=serverAuth\n' >"$cert_dir/ext.cnf"
  openssl x509 -req -in "$cert_dir/tendo.test.csr" -CA "$cert_dir/ca.crt" -CAkey "$temp_dir/ca.key" -CAcreateserial -out "$cert_dir/tendo.test.crt" -days 1 -extfile "$cert_dir/ext.cnf" >/dev/null 2>&1
  chmod 644 "$cert_dir/tendo.test.key" "$cert_dir/tendo.test.crt"
  cp "$cert_dir/ca.crt" "$cert_dir/smoke-ca.crt"
  openssl req -newkey rsa:2048 -nodes -keyout "$cert_dir/oidc.test.key" -out "$cert_dir/oidc.test.csr" -subj '/CN=oidc.test' >/dev/null 2>&1
  printf 'subjectAltName=DNS:oidc.test\nextendedKeyUsage=serverAuth\n' >"$cert_dir/oidc-ext.cnf"
  openssl x509 -req -in "$cert_dir/oidc.test.csr" -CA "$cert_dir/ca.crt" -CAkey "$temp_dir/ca.key" -CAcreateserial -out "$cert_dir/oidc.test.crt" -days 1 -extfile "$cert_dir/oidc-ext.cnf" >/dev/null 2>&1
  chmod 644 "$cert_dir/oidc.test.key" "$cert_dir/oidc.test.crt"
  cat >"$temp_dir/compose.yaml" <<EOF
services:
  oidc-provider:
    build:
      context: $root/backend
      dockerfile: test/oidc-provider/Dockerfile
    environment:
      LISTEN_ADDR: 0.0.0.0:8443
      ISSUER_URL: https://oidc.test:8443
      CLIENT_ID: tendo-proxy-smoke
      CLIENT_SECRET: $TENDO_OIDC_CLIENT_SECRET
      REDIRECT_URI: https://tendo.test/api/v1/auth/oidc/callback
      SUBJECTS: owner-subject,unknown-subject
      TLS_CERT_FILE: /certs/oidc.test.crt
      TLS_KEY_FILE: /certs/oidc.test.key
      SSL_CERT_FILE: /certs/ca.crt
    command: ["-listen", "0.0.0.0:8443", "-issuer", "https://oidc.test:8443", "-client-id", "tendo-proxy-smoke", "-client-secret", "$TENDO_OIDC_CLIENT_SECRET", "-redirect-uri", "https://tendo.test/api/v1/auth/oidc/callback", "-subjects", "owner-subject,unknown-subject", "-tls-cert", "/certs/oidc.test.crt", "-tls-key", "/certs/oidc.test.key"]
    ports:
      - "127.0.0.1:$TENDO_OIDC_HOST_PORT:8443"
    volumes:
      - $cert_dir:/certs:ro
    networks:
      proxy-net:
        ipv4_address: 172.29.247.40
        aliases: [oidc.test]
    healthcheck:
      test: ["CMD", "/oidc-provider", "-listen", ":8443", "-issuer", "https://oidc.test:8443", "-tls-cert", "/certs/oidc.test.crt", "-tls-key", "/certs/oidc.test.key", "-healthcheck"]
      interval: 2s
      timeout: 2s
      retries: 3
  migrate:
    networks:
      - proxy-net
  app:
    environment:
      TENDO_PUBLIC_URL: https://tendo.test
      TENDO_OIDC_ISSUER: https://oidc.test:8443
      TENDO_OIDC_CLIENT_ID: tendo-proxy-smoke
      TENDO_OIDC_CLIENT_SECRET: $TENDO_OIDC_CLIENT_SECRET
      TENDO_OIDC_DISPLAY_NAME: Proxy Smoke Provider
      SSL_CERT_FILE: /certs/ca.crt
      TENDO_TRUSTED_PROXY_CIDRS: 172.29.247.10/32
    volumes:
      - $cert_dir/ca.crt:/certs/ca.crt:ro
    ports: !reset []
    networks:
      proxy-net:
        ipv4_address: 172.29.247.20
  postgres:
    networks:
      - proxy-net
  proxy:
    image: $([[ "$proxy" == caddy ]] && printf 'caddy:2.11.7-alpine' || ([[ "$proxy" == nginx ]] && printf 'nginx:1.30.6-alpine' || printf 'traefik:v3.7.16'))
    depends_on:
      app:
        condition: service_started
    volumes:
      - $cert_dir:/certs:ro
      - $smoke_dir:/tmp/smoke
$(case "$proxy" in caddy) printf '      - %s/deploy/reverse-proxy/Caddyfile:/etc/caddy/Caddyfile:ro\n' "$root" ;; nginx) printf '      - %s/deploy/reverse-proxy/nginx.conf:/etc/nginx/nginx.conf:ro\n' "$root" ;; traefik) printf '      - %s/deploy/reverse-proxy/traefik.yml:/etc/traefik/traefik.yml:ro\n      - %s/deploy/reverse-proxy/traefik-dynamic.yml:/etc/traefik/dynamic.yml:ro\n' "$root" "$root" ;; esac)
$(if [[ "$proxy" == traefik ]]; then printf '    command: ["--configFile=/etc/traefik/traefik.yml"]\n'; fi)
    ports:
      - "127.0.0.1:$TENDO_HOST_PORT:443"
    networks:
      proxy-net:
        ipv4_address: 172.29.247.10
networks:
  proxy-net:
    ipam:
      config:
        - subnet: 172.29.247.0/24
EOF

}

expect_status() {
    local expected=$1 path=$2; shift 2
    local actual
    actual=$(curl "${curl_args[@]}" --output "$smoke_dir/body" --write-out '%{http_code}' "$@" "https://tendo.test:$TENDO_HOST_PORT$path")
    [[ "$actual" == "$expected" ]] || { printf 'Expected HTTP %s for %s, got %s\n' "$expected" "$path" "$actual" >&2; return 1; }
}
poll_status() {
    local expected=$1 path=$2 status=connection-failed deadline=$((SECONDS + 60)); shift 2
    while (( SECONDS < deadline )); do
      if status=$(curl "${curl_args[@]}" --output /dev/null --write-out '%{http_code}' "$@" "https://tendo.test:$TENDO_HOST_PORT$path"); then
        [[ "$status" == "$expected" ]] && return 0
      else
        status=connection-failed
      fi
      sleep 1
    done
    printf 'Timed out waiting for %s HTTP %s; last result=%s\n' "$path" "$expected" "$status" >&2
    return 1
}

bring_up_proxy_variant() {
  "${compose[@]}" up -d --build --wait --wait-timeout 150 postgres
  "${migrate[@]}" run --rm migrate
  "${compose[@]}" up -d --build --wait --wait-timeout 150 oidc-provider app proxy
  curl_args=(--silent --show-error --noproxy '*' --max-time 5 --cacert "$cert_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" -H 'Host: tendo.test')
}

probe_http_contract() {
  poll_status 200 /health/live
  expect_status 200 /health/live
  poll_status 200 /health/ready
  expect_status 200 /health/ready
  curl "${curl_args[@]}" -D "$temp_dir/headers" -o "$temp_dir/body" -H 'X-Request-ID: proxy_smoke-01' "https://tendo.test:$TENDO_HOST_PORT/health/ready"
  python3 - "$temp_dir/headers" "$temp_dir/body" <<'PY'
import json, re, sys
headers = open(sys.argv[1], encoding="utf-8").read().lower()
body = json.load(open(sys.argv[2], encoding="utf-8"))
assert body == {"status": "ok"}, body
assert re.search(r"^content-type: application/json(?:;[^\r\n]*)?\r?$", headers, re.M), headers
assert re.search(r"^x-request-id: proxy_smoke-01\r?$", headers, re.M), headers
assert re.search(r"^cache-control: no-store\r?$", headers, re.M), headers
PY
  expect_status 404 /api/not-a-route
  # Proxy access logs must never persist request query strings, which may contain secrets.
  query_secret="proxy-smoke-secret-${RANDOM}-${RANDOM}"
  expect_status 404 "/api/not-a-route?token=$query_secret"
  assert_no_proxy_secret() {
    local secret=$1
    if "${compose[@]}" logs --no-color proxy | python3 -c 'import sys; needle=sys.argv[1]; data=sys.stdin.read(); sys.exit(1 if needle in data else 0)' "$secret"; then
      :
    else
      printf 'Synthetic query secret appeared in %s container logs\n' "$proxy" >&2
      exit 1
    fi
  }
  assert_no_proxy_secret "$query_secret"
  outage_secret="proxy-smoke-outage-${RANDOM}-${RANDOM}"
  "${compose[@]}" stop app >/dev/null
  if [[ "$proxy" == nginx ]]; then
    sleep 1
  fi
  outage_url="https://tendo.test:$TENDO_HOST_PORT/api/not-a-route?token=$outage_secret"
  outage_status=$(curl --silent --show-error --noproxy '*' --max-time 5 --cacert "$cert_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/outage-body" --write-out '%{http_code}' -H 'Host: tendo.test' "$outage_url")
  if [[ "$proxy" == nginx ]]; then
    [[ "$outage_status" == 504 ]] || { printf 'Expected Nginx upstream timeout HTTP 504 while app is stopped, got %s\n' "$outage_status" >&2; exit 1; }
  else
    [[ "$outage_status" == 502 || "$outage_status" == 503 ]] || { printf 'Expected proxy transport failure 502/503 while app is stopped, got %s\n' "$outage_status" >&2; exit 1; }
  fi
  "${compose[@]}" start app >/dev/null
  "${compose[@]}" up -d --wait --wait-timeout 90 app
  poll_status 200 /health/ready
  assert_no_proxy_secret "$outage_secret"
  # TLS SNI remains tendo.test while Host changes; only Nginx serves its default TLS vhost,
  # and must reject rather than rewrite this authority into the canonical app route.
  if [[ "$proxy" == nginx ]]; then
    actual=$(curl --silent --show-error --noproxy '*' --max-time 5 --cacert "$cert_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
    [[ "$actual" == 421 ]] || { printf 'Mismatched TLS SNI/Host expected HTTP 421, got %s\n' "$actual" >&2; exit 1; }
    python3 - "$temp_dir/mismatched-host-body" <<'PY'
import sys
body = open(sys.argv[1], "rb").read()
assert not body or b'"status":404' not in body, body
PY
  elif [[ "$proxy" == traefik ]]; then
    actual=$(curl --silent --show-error --noproxy '*' --http1.1 --max-time 5 --cacert "$cert_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
    [[ "$actual" == 404 ]] || { printf 'Traefik mismatched Host expected router HTTP 404, got %s\n' "$actual" >&2; exit 1; }
    python3 - "$temp_dir/mismatched-host-body" <<'PY'
import json, sys
body = open(sys.argv[1], encoding="utf-8").read()
try:
    problem = json.loads(body)
except json.JSONDecodeError:
    problem = None
assert problem != {"type": "about:blank", "title": "Not Found", "status": 404}, body
PY
  else
    actual=$(curl --silent --show-error --noproxy '*' --http1.1 --max-time 5 --cacert "$cert_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
    if [[ "$actual" != 421 ]]; then
      printf 'Caddy strict SNI/Host mismatch expected proxy HTTP 421, got %s; body=' "$actual" >&2
      python3 -c 'import pathlib,sys; print(repr(pathlib.Path(sys.argv[1]).read_bytes()))' "$temp_dir/mismatched-host-body" >&2
      exit 1
    fi
    python3 - "$temp_dir/mismatched-host-body" <<'PY'
import json, sys
body = open(sys.argv[1], "rb").read()
assert not body or json.loads(body) != {"type": "about:blank", "title": "Not Found", "status": 404}, body
print(f"Caddy mismatched SNI/Host: HTTP 421 proxy rejection; body={body!r}")
PY
  fi
  expect_status 404 /api/not-a-route -H 'X-Forwarded-Proto: http'
  expect_status 404 /api/not-a-route -H 'X-Forwarded-Host: attacker.test'
  expect_status 403 /api/not-a-route -H 'Origin: https://attacker.test'
  expect_status 403 /api/not-a-route -X POST -H 'Origin:'
  expect_status 404 /api/not-a-route -H 'Forwarded: for=192.0.2.4;proto=http;host=attacker.test' -H 'X-Forwarded-Proto: http' -H 'X-Forwarded-Host: attacker.test' -H 'X-Forwarded-For: 192.0.2.4'
  curl "${curl_args[@]}" -D "$temp_dir/error-headers" -o "$temp_dir/error-body" "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route"
  python3 - "$temp_dir/error-headers" "$temp_dir/error-body" <<'PY'
import json, re, sys
headers = open(sys.argv[1], encoding="utf-8").read().lower()
body = json.load(open(sys.argv[2], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Not Found", "status": 404}, body
assert re.search(r"^content-type: application/problem\+json(?:;[^\r\n]*)?\r?$", headers, re.M), headers
assert re.search(r"^cache-control: no-store\r?$", headers, re.M), headers
assert re.search(r"^x-request-id: [a-z0-9._-]{1,64}\r?$", headers, re.M), headers
PY
  # The embedded SPA is served through the proxy with its security headers passed through unchanged.
  curl "${curl_args[@]}" -D "$temp_dir/spa-headers" -o "$temp_dir/spa-body" -w '%{http_code}\n' "https://tendo.test:$TENDO_HOST_PORT/" >"$temp_dir/spa-status"
  python3 - "$temp_dir/spa-status" "$temp_dir/spa-headers" "$temp_dir/spa-body" <<'PY'
import re, sys
status = open(sys.argv[1], encoding="utf-8").read().strip()
headers = open(sys.argv[2], encoding="utf-8").read().lower()
body = open(sys.argv[3], encoding="utf-8").read().lower()
assert status == "200", status
assert re.search(r"^content-type: text/html(?:;[^\r\n]*)?\r?$", headers, re.M), headers
assert re.search(r"^content-security-policy: [^\r\n]*default-src 'self'[^\r\n]*\r?$", headers, re.M), headers
assert re.search(r"^x-frame-options: deny\r?$", headers, re.M), headers
assert '<div id="root">' in body, body
PY

  # Local login through the HTTPS proxy: the cookie policy follows TENDO_PUBLIC_URL (https), not the plain-HTTP backend socket.
}

probe_cookie_and_local_session() {
  base="https://tendo.test:$TENDO_HOST_PORT"
  setup_status=$(curl "${curl_args[@]}" --output "$temp_dir/setup-body" --write-out '%{http_code}' -X PUT -H "Origin: https://tendo.test" -H "X-Tendo-Setup-Token: $TENDO_SETUP_TOKEN" -H 'Content-Type: application/json' --data '{"login":"proxy_owner","password":"correct horse battery","householdName":"Proxy home","timezone":"UTC"}' "$base/api/v1/auth/setup")
  [[ "$setup_status" == 201 ]] || { printf 'Setup through proxy expected HTTP 201, got %s\n' "$setup_status" >&2; exit 1; }
  login_status=$(curl "${curl_args[@]}" -D "$temp_dir/login-headers" --output "$temp_dir/login-body" --write-out '%{http_code}' -X POST -H "Origin: https://tendo.test" -H 'Content-Type: application/json' --data '{"login":"proxy_owner","password":"correct horse battery"}' "$base/api/v1/session")
  python3 - "$login_status" "$temp_dir/login-headers" "$temp_dir/login-body" "$temp_dir/session-cookie" "$secret_inventory" <<'PY'
import json, re, sys
status = int(sys.argv[1])
headers = open(sys.argv[2], encoding="utf-8").read()
cookies = re.findall(r"^set-cookie: ([^\r\n]*)\r?$", headers, re.M | re.I)
with open(sys.argv[5], "a", encoding="utf-8") as inventory:
    for cookie in cookies:
        pair = cookie.split(";", 1)[0]
        if "=" in pair and pair.split("=", 1)[1]:
            inventory.write(pair.split("=", 1)[1] + "\n")
            inventory.flush()
assert status == 201, f"local login returned HTTP {status}"
assert len(cookies) == 1, f"expected one Set-Cookie header, got {len(cookies)}"
parts = [p.strip() for p in cookies[0].split(";")]
name, _, token = parts[0].partition("=")
attrs = [p.lower() for p in parts[1:]]
assert name == "__Host-tendo_session" and re.fullmatch(r"[A-Za-z0-9_-]{43}", token), f"unexpected cookie name/token format: {name}"
assert "secure" in attrs and "httponly" in attrs and "samesite=lax" in attrs and "path=/" in attrs, f"unexpected cookie attribute names: {[a.split('=',1)[0] for a in attrs]}"
assert not any(a.startswith("domain=") for a in attrs), f"unexpected cookie attribute names: {[a.split('=',1)[0] for a in attrs]}"
body = json.load(open(sys.argv[3], encoding="utf-8"))
assert body.get("login") == "proxy_owner" and "defaultHouseholdId" in body and token not in json.dumps(body), f"local login returned unexpected response fields: {sorted(body) if isinstance(body, dict) else type(body).__name__}"
open(sys.argv[4], "w", encoding="utf-8").write(f"{name}={token}")
PY
  session_cookie=$(<"$temp_dir/session-cookie")
  expect_status 200 /api/v1/session -H "Cookie: $session_cookie"
  python3 - "$smoke_dir/body" <<'PY'
import json, sys
assert json.load(open(sys.argv[1], encoding="utf-8"))["login"] == "proxy_owner"
PY
  expect_status 401 /api/v1/session -H "Cookie: tendo_session=${session_cookie#*=}"
  expect_status 401 /api/v1/session

}

probe_oidc_round_trip() {
  OIDC_SMOKE_CA="$cert_dir/ca.crt" OIDC_PROXY_PORT="$TENDO_HOST_PORT" OIDC_PROVIDER_PORT="$TENDO_OIDC_HOST_PORT" OIDC_SESSION_COOKIE="$session_cookie" OIDC_CLIENT_SECRET="$TENDO_OIDC_CLIENT_SECRET" OIDC_SECRET_VALUES="$secret_inventory" python3 "$root/scripts/proxy_smoke_oidc.py"
}

scan_logs_for_secrets() {
  "${compose[@]}" logs --no-color proxy app >"$temp_dir/oidc-logs"
  python3 - "$temp_dir/oidc-logs" "$secret_inventory" <<'PY'
import pathlib, sys
log_path, inventory_path = map(pathlib.Path, sys.argv[1:])
log_text = log_path.read_text(errors='replace')
for index, secret in enumerate(inventory_path.read_text().splitlines()):
    if secret and secret in log_text:
        print(f'Protected value #{index} appeared in proxy/app logs', file=sys.stderr)
        raise SystemExit(1)
PY

}

probe_trusted_peer_headers() {
  "${compose[@]}" stop proxy >/dev/null
  "${compose[@]}" rm -sf proxy >/dev/null
  docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 curlimages/curl:8.23.0 --silent --show-error --max-time 5 -o /dev/null http://172.29.247.20:8080/health/live
  test_trusted_headers() {
    local proto=$1 host=$2 origin=$3 expected=$4
    local -a origin_args=()
    [[ -z "$origin" ]] || origin_args=(-H "Origin: $origin")
    actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 -v "$smoke_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.23.0 \
      --silent --show-error --max-time 5 -o /tmp/smoke/trusted-body -w '%{http_code}' \
      -H "X-Forwarded-Proto: $proto" -H "X-Forwarded-Host: $host" "${origin_args[@]}" \
      "http://172.29.247.20:8080/api/not-a-route")
    [[ "$actual" == "$expected" ]] || { printf 'Trusted peer expected HTTP %s, got %s\n' "$expected" "$actual" >&2; return 1; }
    python3 - "$smoke_dir/trusted-body" "$expected" <<'PY'
import json, sys
body = json.load(open(sys.argv[1], encoding="utf-8"))
expected = int(sys.argv[2])
expected_body = {200: {"status": "ok"}, 404: {"type": "about:blank", "title": "Not Found", "status": 404}, 400: {"type": "about:blank", "title": "Bad Request", "status": 400}, 421: {"type": "about:blank", "title": "Misdirected Request", "status": 421}, 403: {"type": "about:blank", "title": "Forbidden", "status": 403}}[expected]
assert body == expected_body, body
PY
  }
  test_trusted_headers https tendo.test https://tendo.test 404
  test_trusted_headers http tendo.test https://tendo.test 421
  test_trusted_headers https attacker.test https://attacker.test 421
  test_trusted_headers https tendo.test https://attacker.test 403
  test_trusted_headers https tendo.test '' 404
  test_trusted_headers 'https,https' tendo.test https://tendo.test 400
  actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 -v "$smoke_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.23.0 --silent --show-error --max-time 5 -o /tmp/smoke/malformed-forwarded-body -w '%{http_code}' \
    -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: tendo.test' -H 'X-Forwarded-For: not-an-ip, also-not-an-ip' \
    "http://172.29.247.20:8080/api/not-a-route")
  [[ "$actual" == 400 ]] || { printf 'Proxy-stripped malformed X-Forwarded-For expected HTTP 400, got %s\n' "$actual" >&2; exit 1; }
  python3 - "$smoke_dir/malformed-forwarded-body" <<'PY'
import json, sys
body = json.load(open(sys.argv[1], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Bad Request", "status": 400}, body
PY

  actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.30 -v "$smoke_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.23.0 --silent --show-error --max-time 5 -o /tmp/smoke/untrusted-body -w '%{http_code}' \
    -H 'Forwarded: for=192.0.2.1;proto=https;host=tendo.test' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: tendo.test' \
    "http://172.29.247.20:8080/api/not-a-route")
  [[ "$actual" == 421 ]] || { printf 'Untrusted peer expected HTTP 421, got %s\n' "$actual" >&2; exit 1; }
  python3 - "$smoke_dir/untrusted-body" <<'PY'
import json, sys
body = json.load(open(sys.argv[1], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Misdirected Request", "status": 421}, body
PY
  "${compose[@]}" up -d --force-recreate --wait --wait-timeout 30 proxy

}

probe_readiness_outage() {
  "${compose[@]}" stop postgres >/dev/null
  poll_status 503 /health/ready
  status=$(curl "${curl_args[@]}" -D "$temp_dir/unavailable-headers" -o "$temp_dir/unavailable-body" -w '%{http_code}' "https://tendo.test:$TENDO_HOST_PORT/health/ready")
  [[ "$status" == 503 ]] || { printf 'Unavailable readiness expected HTTP 503, got %s\\n' "$status" >&2; exit 1; }
  python3 - "$temp_dir/unavailable-headers" "$temp_dir/unavailable-body" <<'PY'
import json, re, sys
headers = open(sys.argv[1], encoding="utf-8").read().lower()
body = json.load(open(sys.argv[2], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Service Unavailable", "status": 503}, body
assert re.search(r"^content-type: application/problem\+json(?:;[^\r\n]*)?\r?$", headers, re.M), headers
assert re.search(r"^cache-control: no-store\r?$", headers, re.M), headers
assert re.search(r"^x-request-id: [a-z0-9._-]{1,64}\r?$", headers, re.M), headers
PY
  "${compose[@]}" start postgres >/dev/null
  "${compose[@]}" up -d --wait --wait-timeout 90 postgres
  poll_status 200 /health/ready
}

# Shared phase state: setup_environment provides project/temp_dir/cert_dir/smoke_dir,
# compose/migrate, ports and secret_inventory; bring_up_proxy_variant provides curl_args.
# probe_cookie_and_local_session provides base and session_cookie for later session/OIDC probes.
# Later probes intentionally consume these smoke-scoped globals in sequence.
setup_environment
trap cleanup EXIT
setup_certificates_and_compose
bring_up_proxy_variant
probe_http_contract
probe_cookie_and_local_session
probe_oidc_round_trip
scan_logs_for_secrets
probe_trusted_peer_headers
probe_readiness_outage
