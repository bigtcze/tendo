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
project="tendo-proxy-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
proxy=${PROXY:-caddy}
case "$proxy" in
  caddy|nginx|traefik) ;;
  *) printf 'Unknown proxy %s; use caddy, nginx, or traefik\n' "$proxy" >&2; exit 2 ;;
esac
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-proxy-smoke.XXXXXX")
chmod 755 "$temp_dir"
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
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
TENDO_PUBLIC_URL=https://tendo.test
TENDO_TRUSTED_PROXY_CIDRS=
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$temp_dir/compose.yaml")
migrate=(docker compose --profile migration --project-name "$project" -f "$root/compose.yaml" -f "$temp_dir/compose.yaml")
cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    if "${compose[@]}" logs --no-color >"$temp_dir/logs" 2>&1; then
      python3 - "$temp_dir/logs" <<'PY'
import os, re, sys
text = open(sys.argv[1], encoding="utf-8").read()
for name in ("POSTGRES_PASSWORD", "TENDO_DATABASE_PASSWORD", "TENDO_OIDC_CLIENT_SECRET"):
    text = text.replace(os.environ[name], "[REDACTED]")
text = re.sub(r"(?i)(POSTGRES_PASSWORD|TENDO_DATABASE_PASSWORD|TENDO_OIDC_CLIENT_SECRET)(=|%3[dD])[^\s&]+", r"\1\2[REDACTED]", text)
print(text, end="", file=sys.stderr)
PY
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
trap cleanup EXIT

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$temp_dir/ca.key" -out "$temp_dir/ca.crt" -subj '/CN=Tendo proxy smoke CA' -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
chmod 644 "$temp_dir/ca.crt"
openssl req -newkey rsa:2048 -nodes -keyout "$temp_dir/tendo.test.key" -out "$temp_dir/tendo.test.csr" -subj '/CN=tendo.test' >/dev/null 2>&1
printf 'subjectAltName=DNS:tendo.test\nextendedKeyUsage=serverAuth\n' >"$temp_dir/ext.cnf"
openssl x509 -req -in "$temp_dir/tendo.test.csr" -CA "$temp_dir/ca.crt" -CAkey "$temp_dir/ca.key" -CAcreateserial -out "$temp_dir/tendo.test.crt" -days 1 -extfile "$temp_dir/ext.cnf" >/dev/null 2>&1
cp "$temp_dir/ca.crt" "$temp_dir/smoke-ca.crt"
openssl req -newkey rsa:2048 -nodes -keyout "$temp_dir/oidc.test.key" -out "$temp_dir/oidc.test.csr" -subj '/CN=oidc.test' >/dev/null 2>&1
printf 'subjectAltName=DNS:oidc.test\nextendedKeyUsage=serverAuth\n' >"$temp_dir/oidc-ext.cnf"
openssl x509 -req -in "$temp_dir/oidc.test.csr" -CA "$temp_dir/ca.crt" -CAkey "$temp_dir/ca.key" -CAcreateserial -out "$temp_dir/oidc.test.crt" -days 1 -extfile "$temp_dir/oidc-ext.cnf" >/dev/null 2>&1
chmod 644 "$temp_dir/oidc.test.key" "$temp_dir/oidc.test.crt"
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
      - $temp_dir:/certs:ro
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
      - $temp_dir:/certs:ro
    ports: !reset []
    networks:
      proxy-net:
        ipv4_address: 172.29.247.20
  postgres:
    networks:
      - proxy-net
  proxy:
    image: $([[ "$proxy" == caddy ]] && printf 'caddy:2.11.7-alpine' || ([[ "$proxy" == nginx ]] && printf 'nginx:1.30.5-alpine' || printf 'traefik:v3.7.14'))
    depends_on:
      app:
        condition: service_started
    volumes:
      - $temp_dir:/certs:ro
      - $temp_dir:/tmp/smoke
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

"${compose[@]}" up -d --build --wait --wait-timeout 150 postgres
"${migrate[@]}" run --rm migrate
"${compose[@]}" up -d --build --wait --wait-timeout 150 oidc-provider app proxy
curl_args=(--silent --show-error --noproxy '*' --max-time 5 --cacert "$temp_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" -H 'Host: tendo.test')
expect_status() {
  local expected=$1 path=$2; shift 2
  local actual
  actual=$(curl "${curl_args[@]}" --output "$temp_dir/body" --write-out '%{http_code}' "$@" "https://tendo.test:$TENDO_HOST_PORT$path")
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
outage_status=$(curl --silent --show-error --noproxy '*' --max-time 5 --cacert "$temp_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/outage-body" --write-out '%{http_code}' -H 'Host: tendo.test' "$outage_url")
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
  actual=$(curl --silent --show-error --noproxy '*' --max-time 5 --cacert "$temp_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
  [[ "$actual" == 421 ]] || { printf 'Mismatched TLS SNI/Host expected HTTP 421, got %s\n' "$actual" >&2; exit 1; }
  python3 - "$temp_dir/mismatched-host-body" <<'PY'
import sys
body = open(sys.argv[1], "rb").read()
assert not body or b'"status":404' not in body, body
PY
elif [[ "$proxy" == traefik ]]; then
  actual=$(curl --silent --show-error --noproxy '*' --http1.1 --max-time 5 --cacert "$temp_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
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
  actual=$(curl --silent --show-error --noproxy '*' --http1.1 --max-time 5 --cacert "$temp_dir/ca.crt" --resolve "tendo.test:$TENDO_HOST_PORT:127.0.0.1" --output "$temp_dir/mismatched-host-body" --write-out '%{http_code}' -H 'Host: attacker.test' "https://tendo.test:$TENDO_HOST_PORT/api/not-a-route")
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
base="https://tendo.test:$TENDO_HOST_PORT"
setup_status=$(curl "${curl_args[@]}" --output "$temp_dir/setup-body" --write-out '%{http_code}' -X PUT -H "Origin: https://tendo.test" -H "X-Tendo-Setup-Token: $TENDO_SETUP_TOKEN" -H 'Content-Type: application/json' --data '{"login":"proxy_owner","password":"correct horse battery","householdName":"Proxy home","timezone":"UTC"}' "$base/api/v1/auth/setup")
[[ "$setup_status" == 201 ]] || { printf 'Setup through proxy expected HTTP 201, got %s\n' "$setup_status" >&2; exit 1; }
login_status=$(curl "${curl_args[@]}" -D "$temp_dir/login-headers" --output "$temp_dir/login-body" --write-out '%{http_code}' -X POST -H "Origin: https://tendo.test" -H 'Content-Type: application/json' --data '{"login":"proxy_owner","password":"correct horse battery"}' "$base/api/v1/session")
[[ "$login_status" == 201 ]] || { printf 'Login through proxy expected HTTP 201, got %s\n' "$login_status" >&2; exit 1; }
python3 - "$temp_dir/login-headers" "$temp_dir/login-body" "$temp_dir/session-cookie" <<'PY'
import json, re, sys
headers = open(sys.argv[1], encoding="utf-8").read()
cookies = re.findall(r"^set-cookie: ([^\r\n]*)\r?$", headers, re.M | re.I)
assert len(cookies) == 1, headers
parts = [p.strip() for p in cookies[0].split(";")]
name, _, token = parts[0].partition("=")
attrs = [p.lower() for p in parts[1:]]
assert name == "__Host-tendo_session" and re.fullmatch(r"[A-Za-z0-9_-]{43}", token), parts[0]
assert "secure" in attrs and "httponly" in attrs and "samesite=lax" in attrs and "path=/" in attrs, attrs
assert not any(a.startswith("domain=") for a in attrs), attrs
body = json.load(open(sys.argv[2], encoding="utf-8"))
assert body["login"] == "proxy_owner" and "defaultHouseholdId" in body and token not in json.dumps(body), body
open(sys.argv[3], "w", encoding="utf-8").write(f"{name}={token}")
PY
session_cookie=$(<"$temp_dir/session-cookie")
expect_status 200 /api/v1/session -H "Cookie: $session_cookie"
python3 - "$temp_dir/body" <<'PY'
import json, sys
assert json.load(open(sys.argv[1], encoding="utf-8"))["login"] == "proxy_owner"
PY
expect_status 401 /api/v1/session -H "Cookie: tendo_session=${session_cookie#*=}"
expect_status 401 /api/v1/session

OIDC_SMOKE_CA="$temp_dir/ca.crt" OIDC_PROXY_PORT="$TENDO_HOST_PORT" OIDC_PROVIDER_PORT="$TENDO_OIDC_HOST_PORT" OIDC_SESSION_COOKIE="$session_cookie" OIDC_CLIENT_SECRET="$TENDO_OIDC_CLIENT_SECRET" OIDC_SECRET_VALUES="$temp_dir/oidc-secret-values" python3 - <<'PY'
import http.client, json, os, ssl, urllib.parse

ca = os.environ['OIDC_SMOKE_CA']
context = ssl.create_default_context(cafile=ca)
class VerifiedConnection(http.client.HTTPSConnection):
    def connect(self):
        sock = self._create_connection(('127.0.0.1', self.port), self.timeout, self.source_address)
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)

def request(host, port, method, path, headers=None, body=None):
    conn = VerifiedConnection(host, int(port), context=context, timeout=12)
    encoded = json.dumps(body).encode() if body is not None else None
    h = dict(headers or {})
    if body is not None: h['Content-Type'] = 'application/json'
    conn.request(method, path, body=encoded, headers=h)
    r = conn.getresponse(); response_headers = r.headers; raw = r.read(); conn.close()
    parsed = json.loads(raw) if raw and response_headers.get_content_type() == 'application/json' else raw
    return r.status, response_headers, parsed

def cookies(headers): return headers.get_all('Set-Cookie') or []
def named_cookie(headers, name):
    found = [v for v in cookies(headers) if v.split(';',1)[0].startswith(name+'=')]
    assert len(found) == 1, (name, cookies(headers))
    return found[0].split(';',1)[0]
def secure_session(headers):
    value = named_cookie(headers, '__Host-tendo_session')
    cookie = next(v for v in cookies(headers) if v.startswith(value+';'))
    attrs = [x.strip().lower() for x in cookie.split(';')[1:]]
    assert 'secure' in attrs and 'httponly' in attrs and 'path=/' in attrs and 'samesite=lax' in attrs and not any(x.startswith('domain=') for x in attrs), cookie
    return value

def start(purpose, session=None, password=None, forged=False):
    b = {'purpose': purpose}
    if password is not None: b['currentPassword'] = password
    hs = {'Origin':'https://tendo.test'}
    if session: hs['Cookie'] = session
    if forged: hs.update({'X-Forwarded-Host':'attacker.test','X-Forwarded-Proto':'http','Forwarded':'host=attacker.test;proto=http'})
    status, h, data = request('tendo.test', os.environ['OIDC_PROXY_PORT'], 'POST', '/api/v1/auth/oidc/start', hs, b)
    assert status == 200, ('start',status,data)
    url = urllib.parse.urlsplit(data['authorizationUrl'])
    assert url.scheme == 'https' and url.netloc == 'oidc.test:8443', data
    query = urllib.parse.parse_qs(url.query, strict_parsing=True)
    assert query['redirect_uri'] == ['https://tendo.test/api/v1/auth/oidc/callback'], query
    flow = named_cookie(h, '__Host-tendo_oidc')
    fc = next(v for v in cookies(h) if v.startswith(flow+';'))
    attrs = [x.strip().lower() for x in fc.split(';')[1:]]
    assert 'secure' in attrs and 'httponly' in attrs and 'path=/' in attrs and any(x.startswith('samesite=') for x in attrs) and not any(x.startswith('domain=') for x in attrs), fc
    secrets.extend([flow.split('=',1)[1], query['state'][0], query.get('nonce',[''])[0], query.get('code_challenge',[''])[0]])
    return query, flow

def authorize(query, sub='owner-subject'):
    q = dict(query); q['sub'] = [sub]
    path = '/authorize?' + urllib.parse.urlencode(q, doseq=True)
    status,h,_ = request('oidc.test',os.environ['OIDC_PROVIDER_PORT'],'GET',path)
    location = h.get('Location','')
    assert status == 302 and location.startswith('https://tendo.test/api/v1/auth/oidc/callback?'), (status,location)
    parsed = urllib.parse.urlsplit(location)
    secrets.extend(urllib.parse.parse_qs(parsed.query).get('code',[]))
    return parsed

def callback(parsed, flow, session=None, flow_cookie_name='__Host-tendo_oidc'):
    hs={'Cookie':flow_cookie_name+'='+flow.split('=',1)[1]+('; '+session if session else '')}
    return request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET',parsed.path+'?'+parsed.query,hs)

secrets = [os.environ['OIDC_CLIENT_SECRET'], os.environ['OIDC_SESSION_COOKIE'].split('=',1)[1]]
s,h,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/auth/oidc')
assert s == 200 and b == {'enabled':True,'displayName':'Proxy Smoke Provider'}, (s,b)
old = os.environ['OIDC_SESSION_COOKIE']
q,flow = start('link',old,'correct horse battery',True)
parsed = authorize(q)
s,h,b = callback(parsed,flow,old)
assert s == 303 and h.get('Location') == '/account#oidc=connected' and h.get('Cache-Control') == 'no-store', (s,h.get('Location'),dict(h))
assert any(v.startswith('__Host-tendo_oidc=') and 'Max-Age=0' in v for v in cookies(h)),cookies(h)
new = secure_session(h)
secrets.append(new.split('=',1)[1])
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':old})
assert s == 401, ('old session',s,b)
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':new})
assert s == 200 and b['login']=='proxy_owner',(s,b)
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/auth/oidc/identity',{'Cookie':new})
assert s == 200 and b == {'linked':True},(s,b)
s,_,_ = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'DELETE','/api/v1/session',{'Origin':'https://tendo.test','Cookie':new})
assert s == 204, ('logout',s)
q,flow = start('login'); parsed = authorize(q); s,h,b = callback(parsed,flow)
assert s == 303 and h.get('Location') == '/', (s,h.get('Location'))
login_cookie = secure_session(h)
secrets.append(login_cookie.split('=',1)[1])
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':login_cookie})
assert s == 200 and b['login']=='proxy_owner',(s,b)
q,flow = start('login'); parsed = authorize(q); s,h,b = callback(parsed,flow,flow_cookie_name='tendo_oidc')
assert s == 303 and h.get('Location') == '/login#oidcError=invalid_flow', (s,h.get('Location'))
assert not any(v.startswith('__Host-tendo_session=') for v in cookies(h)),cookies(h)
secrets.append(flow.split('=',1)[1])
with open(os.environ['OIDC_SECRET_VALUES'],'w',encoding='utf-8') as f:
    f.write('\n'.join(x for x in secrets if x)+'\n')
print('HTTPS proxy OIDC link/login/cookie checks passed')
PY
while IFS= read -r oidc_secret; do
  [[ -z "$oidc_secret" ]] && continue
  if "${compose[@]}" logs --no-color proxy app | grep -F "$oidc_secret" >/dev/null; then
    printf 'OIDC code, state, cookie, or client secret appeared in proxy/app logs\n' >&2
    exit 1
  fi
done <"$temp_dir/oidc-secret-values"

"${compose[@]}" stop proxy >/dev/null
"${compose[@]}" rm -sf proxy >/dev/null
docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 curlimages/curl:8.22.0 --silent --show-error --max-time 5 -o /dev/null http://172.29.247.20:8080/health/live
test_trusted_headers() {
  local proto=$1 host=$2 origin=$3 expected=$4
  local -a origin_args=()
  [[ -z "$origin" ]] || origin_args=(-H "Origin: $origin")
  actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 -v "$temp_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.22.0 \
    --silent --show-error --max-time 5 -o /tmp/smoke/trusted-body -w '%{http_code}' \
    -H "X-Forwarded-Proto: $proto" -H "X-Forwarded-Host: $host" "${origin_args[@]}" \
    "http://172.29.247.20:8080/api/not-a-route")
  [[ "$actual" == "$expected" ]] || { printf 'Trusted peer expected HTTP %s, got %s\n' "$expected" "$actual" >&2; return 1; }
  python3 - "$temp_dir/trusted-body" "$expected" <<'PY'
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
actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.10 -v "$temp_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.22.0 --silent --show-error --max-time 5 -o /tmp/smoke/malformed-forwarded-body -w '%{http_code}' \
  -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: tendo.test' -H 'X-Forwarded-For: not-an-ip, also-not-an-ip' \
  "http://172.29.247.20:8080/api/not-a-route")
[[ "$actual" == 400 ]] || { printf 'Proxy-stripped malformed X-Forwarded-For expected HTTP 400, got %s\n' "$actual" >&2; exit 1; }
python3 - "$temp_dir/malformed-forwarded-body" <<'PY'
import json, sys
body = json.load(open(sys.argv[1], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Bad Request", "status": 400}, body
PY

actual=$(docker run --rm --network "${project}_proxy-net" --ip 172.29.247.30 -v "$temp_dir:/tmp/smoke" --user 0:0 curlimages/curl:8.22.0 --silent --show-error --max-time 5 -o /tmp/smoke/untrusted-body -w '%{http_code}' \
  -H 'Forwarded: for=192.0.2.1;proto=https;host=tendo.test' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Host: tendo.test' \
  "http://172.29.247.20:8080/api/not-a-route")
[[ "$actual" == 421 ]] || { printf 'Untrusted peer expected HTTP 421, got %s\n' "$actual" >&2; exit 1; }
python3 - "$temp_dir/untrusted-body" <<'PY'
import json, sys
body = json.load(open(sys.argv[1], encoding="utf-8"))
assert body == {"type": "about:blank", "title": "Misdirected Request", "status": 421}, body
PY
"${compose[@]}" up -d --force-recreate --wait --wait-timeout 30 proxy

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
