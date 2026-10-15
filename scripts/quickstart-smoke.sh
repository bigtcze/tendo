#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# Local uncommitted changes are intentionally not copied: this is a real clone of the tested checkout's HEAD.
for name in POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_SETUP_TOKEN TENDO_RESTART_POLICY TENDO_TEST_CLOCK_NOW TENDO_TEST_CLOCK_ACK; do
  unset "$name"
done
while IFS= read -r name; do unset "$name"; done < <(compgen -e | grep -E '^TENDO_(OIDC_|TEST_CLOCK_)' || true)
export COMPOSE_PROJECT_NAME="tendo-quickstart-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/tendo-quickstart.XXXXXX")
chmod 700 "$tmp"
clone="$tmp/tendo"
result=0
redacted_compose_logs() {
  local env_file="$clone/.env" logs
  logs=$(docker compose logs --no-color 2>&1) || true
  if [[ -f $env_file ]]; then
    while IFS='=' read -r key value; do
      case "$key" in POSTGRES_PASSWORD|TENDO_DATABASE_PASSWORD|TENDO_SETUP_TOKEN) [[ -n $value ]] && logs=${logs//"$value"/[REDACTED]} ;; esac
    done < "$env_file"
  fi
  printf '%s\n' "$logs" >&2
}
cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )) && [[ -d $clone ]]; then
    (cd "$clone" && redacted_compose_logs) || printf 'Could not collect redacted Compose logs\n' >&2
  fi
  if [[ -d $clone ]]; then
    if ! (cd "$clone" && docker compose down --volumes --remove-orphans --rmi local); then
      printf 'Quick-start Compose cleanup failed\n' >&2
      (( result != 0 )) || result=1
    fi
  fi
  if ! rm -rf "$tmp"; then
    printf 'Quick-start temporary directory cleanup failed\n' >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT

python3 - "$root" "$tmp/blocks.json" <<'PY'
import json, pathlib, re, sys
root=pathlib.Path(sys.argv[1])
def sections(path, headings, expected):
    text=(root/path).read_text(encoding='utf-8')
    lines=text.splitlines()
    found={}
    for heading in headings:
        try: start=next(i for i,line in enumerate(lines) if line.strip()==heading)
        except StopIteration: raise SystemExit(f'{path}: required heading missing: {heading}')
        end=next((i for i in range(start+1,len(lines)) if re.match(r'^#{1,2} ',lines[i])),len(lines))
        section='\n'.join(lines[start+1:end])
        blocks=re.findall(r'```sh\s*\n(.*?)\n```',section,re.S)
        if len(blocks)!=expected[heading]: raise SystemExit(f'{path}: {heading}: expected {expected[heading]} sh block(s), found {len(blocks)}')
        found[heading]=blocks
    return found
install_headings=['## 1. Get Tendo','## 2. Create your settings file','## 3. Start Tendo','## 4. Create your household','## 5. Switch setup off']
install=sections('docs/admin/install.md',install_headings,dict(zip(install_headings,[1,2,2,1,1])))
readme_headings=['## Quick start','## First sign-in']
readme=sections('README.md',readme_headings,{readme_headings[0]:1,readme_headings[1]:2})
def commands(block):
    joined=re.sub(r'\\\s*\n\s*',' ',block)
    return [line.strip() for line in joined.splitlines() if line.strip()]
install_commands={cmd for blocks in install.values() for block in blocks for cmd in commands(block)}
for heading,blocks in readme.items():
    for block in blocks:
        for command in commands(block):
            if command not in install_commands:
                raise SystemExit(f'README.md {heading}: command is absent from docs/admin/install.md sh blocks: {command}')
get=install[install_headings[0]][0]
old='git clone https://github.com/bigtcze/tendo.git'
if get.splitlines().count(old)!=1: raise SystemExit('docs/admin/install.md Get Tendo: expected exactly one exact GitHub clone command line')
install[install_headings[0]][0]=get.replace(old, f'git clone --quiet --no-hardlinks --depth 1 file://{root} tendo', 1)
blocks=[install[h][i] for h,n in zip(install_headings,[1,2,2,1,1]) for i in range(n)]
blocks.append(install[install_headings[2]][1])
pathlib.Path(sys.argv[2]).write_text(json.dumps(blocks),encoding='utf-8')
PY
mapfile -d '' -t blocks < <(python3 - "$tmp/blocks.json" <<'PY'
import json,sys
for block in json.load(open(sys.argv[1],encoding='utf-8')): sys.stdout.write(block+'\0')
PY
)

if python3 - <<'PY'
import socket,sys
s=socket.socket(); s.settimeout(1)
try: sys.exit(1 if s.connect_ex(('127.0.0.1',8080)) == 0 else 0)
finally: s.close()
PY
then :; else printf 'Quick-start smoke cannot use documented port 8080: 127.0.0.1:8080 is already in use\n' >&2; exit 1; fi

(cd "$tmp" && bash -euo pipefail -c "${blocks[0]}")
[[ -d $clone ]] || { printf 'Get Tendo block did not create %s\n' "$clone" >&2; exit 1; }
(cd "$clone" && bash -euo pipefail -c "${blocks[1]}" && bash -euo pipefail -c "${blocks[2]}")
python3 - "$clone/.env" "$clone" <<'PY'
import base64,os,re,subprocess,sys
p=sys.argv[1]; mode=os.stat(p).st_mode & 0o777
if mode!=0o600: raise SystemExit(f'.env mode must be 0600, got {mode:04o}; check documented umask command')
values={}
for line in open(p,encoding='utf-8'):
    if '=' in line:
        k,v=line.rstrip('\n').split('=',1); values[k]=v
for key in ('POSTGRES_PASSWORD','TENDO_DATABASE_PASSWORD'):
    if not re.fullmatch(r'[0-9a-f]{64}',values.get(key,'')): raise SystemExit(f'{key} must be 64 lowercase hexadecimal characters')
if values['POSTGRES_PASSWORD']==values['TENDO_DATABASE_PASSWORD']: raise SystemExit('database passwords must differ')
token=values.get('TENDO_SETUP_TOKEN','')
try: decoded=base64.b64decode(token,validate=True)
except Exception: decoded=b''
if len(token)!=44 or len(decoded)!=32 or base64.b64encode(decoded).decode()!=token: raise SystemExit('setup token must be canonical base64 encoding of 32 bytes (44 characters)')
if subprocess.run(['git','-C',sys.argv[2],'status','--porcelain'],capture_output=True,text=True,check=True).stdout: raise SystemExit('fresh clone has a non-empty git status; expected .env to be ignored')
PY

(cd "$clone" && bash -euo pipefail -c "${blocks[3]}")
ready_output=
for ((elapsed=0; elapsed<=60; elapsed++)); do
  if ready_output=$(cd "$clone" && bash -euo pipefail -c "${blocks[4]}" 2>/dev/null); then
    [[ $ready_output == '{"status":"ok"}' ]] && break
  fi
  if (( elapsed == 60 )); then printf 'Health check did not return exactly {"status":"ok"} within 60 seconds\n' >&2; exit 1; fi
  sleep 1
done
[[ $ready_output == '{"status":"ok"}' ]] || { printf 'Health check returned an unexpected response\n' >&2; exit 1; }
token=$(cd "$clone" && bash -euo pipefail -c "${blocks[5]}")
[[ -n $token ]] || { printf 'Setup-code command returned an empty token\n' >&2; exit 1; }
TOKEN="$token" CLONE_ENV="$clone/.env" python3 - <<'PY'
import json,os,urllib.error,urllib.request
origin='http://localhost:8080'; token=os.environ['TOKEN']
values={}
for line in open(os.environ['CLONE_ENV'],encoding='utf-8'):
 if '=' in line:
  k,v=line.rstrip('\n').split('=',1);values[k]=v
if token != values.get('TENDO_SETUP_TOKEN'): raise SystemExit('setup-code command output does not match TENDO_SETUP_TOKEN in .env')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs): return None
opener=urllib.request.build_opener(NoRedirect)
def req(method,path,body=None,headers=None):
 data=json.dumps(body,ensure_ascii=False).encode() if body is not None else None
 h=dict(headers or {})
 if body is not None:h['Content-Type']='application/json'
 request=urllib.request.Request(origin+path,data=data,headers=h,method=method)
 try:
  with opener.open(request,timeout=15) as r:return r.status,r.headers,r.read()
 except urllib.error.HTTPError as e:return e.code,e.headers,e.read()
body={'login':'quickstart_owner','password':'quickstart smoke secure password','householdName':'Novákovi','timezone':'Europe/Prague'}
status,_,raw=req('PUT','/api/v1/auth/setup',body,{'Origin':origin,'X-Tendo-Setup-Token':token})
if status!=201 or json.loads(raw)!= {'required':False}:raise SystemExit(f'Browser setup API expected 201 {{"required":false}}, got {status}')
status,headers,raw=req('POST','/api/v1/session',{'login':body['login'],'password':body['password']},{'Origin':origin})
if status!=201:raise SystemExit(f'Login API expected 201, got {status}')
cookie=headers.get('Set-Cookie','').split(';',1)[0]
if not cookie:raise SystemExit('Login response did not set the session cookie')
session=json.loads(raw); hid=session.get('defaultHouseholdId')
if not hid:raise SystemExit('Login response did not include defaultHouseholdId')
status,_,raw=req('GET','/api/v1/households/'+hid,headers={'Cookie':cookie})
house=json.loads(raw) if raw else {}
if status!=200 or house.get('name')!=body['householdName'] or house.get('timezone')!=body['timezone']:raise SystemExit(f'Household GET mismatch: status={status}')
status,headers,raw=req('GET','/',headers={'Cookie':cookie})
if status!=200 or 'text/html' not in headers.get('Content-Type',''):raise SystemExit(f'Embedded UI GET expected 200 text/html, got {status}')
PY
(cd "$clone" && bash -euo pipefail -c "${blocks[6]}" && bash -euo pipefail -c "${blocks[7]}")
ready_output=
for ((elapsed=0; elapsed<=60; elapsed++)); do
  if ready_output=$(curl --fail --show-error --silent http://localhost:8080/health/ready 2>/dev/null) && [[ $ready_output == '{"status":"ok"}' ]]; then break; fi
  if (( elapsed == 60 )); then printf 'App did not become ready after setup was switched off\n' >&2; exit 1; fi
  sleep 1
done
app_id=$(cd "$clone" && docker compose ps -q app)
[[ -n $app_id ]] || { printf 'docker compose ps -q app returned no app container\n' >&2; exit 1; }
docker inspect "$app_id" | python3 -c 'import json,sys; e=json.load(sys.stdin)[0]["Config"]["Env"]; assert "TENDO_SETUP_TOKEN=" in e, "app container environment must contain an empty TENDO_SETUP_TOKEN"'
TOKEN="$token" python3 - <<'PY'
import json,os,urllib.error,urllib.request
origin='http://localhost:8080';token=os.environ['TOKEN']
def req(method,path,body=None,headers=None):
 data=json.dumps(body).encode() if body is not None else None;h=dict(headers or {})
 if body is not None:h['Content-Type']='application/json'
 r=urllib.request.Request(origin+path,data=data,headers=h,method=method)
 try:
  with urllib.request.urlopen(r,timeout=15) as x:return x.status,x.headers,x.read()
 except urllib.error.HTTPError as e:return e.code,e.headers,e.read()
status,_,raw=req('GET','/api/v1/auth/setup')
if status!=200 or json.loads(raw)!={'required':False}:raise SystemExit('Setup status should remain complete after disabling setup')
status,headers,_=req('POST','/api/v1/session',{'login':'quickstart_owner','password':'quickstart smoke secure password'},{'Origin':origin})
if status!=201:raise SystemExit(f'Login after setup disable expected 201, got {status}')
status,_,raw=req('PUT','/api/v1/auth/setup',{'login':'another_owner','password':'quickstart smoke secure password','householdName':'Not allowed','timezone':'Europe/Prague'},{'Origin':origin,'X-Tendo-Setup-Token':token})
if status!=503:raise SystemExit(f'Disabled setup PUT expected exact status 503, got {status}')
PY
printf 'Quick-start smoke passed: documented install and README commands, first-owner API setup, login, household persistence, embedded UI, and disabled setup.\n'
