#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-oidc-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_OIDC_HOST_PORT OIDC_SMOKE_BACKEND="$root/backend"
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
TENDO_HOST_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
TENDO_OIDC_HOST_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.oidc-test.yaml")
fixtures=$(mktemp)
secrets=$(mktemp)
logs=$(mktemp)
cleanup() {
  local result=$?
  trap - EXIT
  if (( result != 0 )); then
    "${compose[@]}" logs --no-color app >"$logs" 2>/dev/null || true
    redacted_logs=$(mktemp)
    python3 - "$logs" "$secrets" "$redacted_logs" <<'PY'
import pathlib,sys
text=pathlib.Path(sys.argv[1]).read_text(errors='replace')
for secret in pathlib.Path(sys.argv[2]).read_text().splitlines():
 if secret: text=text.replace(secret,'[REDACTED]')
pathlib.Path(sys.argv[3]).write_text(text)
PY
    cat "$redacted_logs" >&2
    rm -f "$redacted_logs"
  fi
  if ! "${compose[@]}" down --volumes --remove-orphans --rmi local; then
    echo 'OIDC Compose cleanup failed' >&2
    (( result != 0 )) || result=1
  fi
  rm -f "$fixtures" "$secrets" "$logs" || { echo 'OIDC smoke temporary cleanup failed' >&2; (( result != 0 )) || result=1; }
  exit "$result"
}
trap cleanup EXIT
"${compose[@]}" up -d --build --wait --wait-timeout 180 postgres oidc-provider
"${compose[@]}" up -d --build --wait --wait-timeout 180
origin=$TENDO_PUBLIC_URL
ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null; then ready=1; break; fi
  sleep 1
done
(( ready )) || { echo 'OIDC app failed readiness' >&2; exit 1; }
OIDC_SMOKE_ORIGIN="$origin" OIDC_SMOKE_FIXTURES="$fixtures" OIDC_HOST_PORT="$TENDO_OIDC_HOST_PORT" OIDC_SMOKE_PSQL="${compose[*]} exec -T postgres psql -U postgres -d tendo -At" OIDC_SMOKE_SECRETS="$secrets" python3 - <<'PY'
import http.client,json,os,subprocess,urllib.error,urllib.parse,urllib.request
origin=os.environ['OIDC_SMOKE_ORIGIN']; hostport=os.environ['OIDC_HOST_PORT']; fixtures=[]
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs): return None
opener=urllib.request.build_opener(NoRedirect)
def request(method,path,body=None,headers=None,record=True):
 hs={**(headers or {})}; data=None
 if body is not None: hs['Content-Type']='application/json'; data=json.dumps(body).encode()
 r=urllib.request.Request(origin+path,data=data,headers=hs,method=method)
 try:
  with opener.open(r,timeout=12) as x: status,h,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e: status,h,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 with open(os.environ['OIDC_SMOKE_SECRETS'],'a',encoding='utf-8') as sf:
  for cookie in h.get_all('Set-Cookie') or []:
   value=cookie.split(';',1)[0].split('=',1)[-1]
   if value: sf.write(value+'\n')
 if record:
  template=path.split('?',1)[0]
  if template.startswith('/api/v1/households/') and '/api/v1/households/{householdId}/' not in template:
   template='/api/v1/households/{householdId}'
  fixture={'path':template,'method':method.lower(),'status':status,'headers':{k.lower():(h.get_all(k) if k.lower()=='set-cookie' else h.get_all(k)[0]) for k in h.keys()}}
  if raw: fixture['body']=result
  fixtures.append(fixture)
 return status,h,result,raw
def provider_get(url):
 u=urllib.parse.urlsplit(url); conn=http.client.HTTPConnection('127.0.0.1',hostport,timeout=12)
 path=(u.path or '/')+('?' + u.query if u.query else '')
 conn.request('GET',path,headers={'Host':u.netloc})
 response=conn.getresponse(); headers=response.headers; body=response.read(); conn.close()
 return response.status,headers,body
def session_cookie(headers):
 for value in headers.get_all('Set-Cookie') or []:
  first=value.split(';',1)[0]
  if first.startswith('tendo_session='): return first
 raise AssertionError(('missing session cookie',headers.get_all('Set-Cookie')))
def flow_cookie(headers):
 for value in headers.get_all('Set-Cookie') or []:
  first=value.split(';',1)[0]
  if first.startswith('tendo_oidc='): return first
 raise AssertionError(('missing flow cookie',headers.get_all('Set-Cookie')))
def start(purpose,password=None,origin_header=origin,session=None):
 body={'purpose':purpose}
 if password is not None: body['currentPassword']=password
 hs={'Origin':origin_header}
 if session: hs['Cookie']=session
 return request('POST','/api/v1/auth/oidc/start',body,hs)
def auth_start(purpose,session=None,sub=None,deny=False,password=None):
 status,h,b,_=start(purpose,password,session=session)
 if status!=200: return status,h,b,None,None
 flow=flow_cookie(h); authorization=urllib.parse.urlsplit(b['authorizationUrl'])
 assert authorization.scheme=='http' and authorization.netloc=='oidc.test:8089',b
 q=urllib.parse.parse_qs(authorization.query,strict_parsing=True)
 if sub: q['sub']=[sub]
 if deny: q['deny']=['1']
 code,hdr,body=provider_get('http://oidc.test:8089/authorize?'+urllib.parse.urlencode(q,doseq=True))
 assert code==302,('provider authorize',code,body)
 callback=hdr.get('Location'); parsed=urllib.parse.urlsplit(callback)
 assert parsed.scheme=='http' and parsed.netloc=='localhost:'+origin.rsplit(':',1)[1],callback
 cb_headers={'Cookie':flow+('; '+session if session else '')}
 with open(os.environ['OIDC_SMOKE_SECRETS'],'a',encoding='utf-8') as sf:
  sf.write(flow.split('=',1)[1]+'\n')
  if session: sf.write(session.split('=',1)[1]+'\n')
  sf.write(q['state'][0]+'\n')
 cb_status,cbh,cbb,raw=request('GET',parsed.path+'?'+parsed.query,headers=cb_headers)
 with open(os.environ['OIDC_SMOKE_SECRETS'],'a',encoding='utf-8') as sf: sf.write(urllib.parse.parse_qs(parsed.query).get('code',[''])[0]+'\n')
 return cb_status,cbh,cbb,(parsed.path+'?'+parsed.query),flow

status,h,b,_=request('PUT','/api/v1/auth/setup',{'login':'oidc_owner','password':'correct horse battery','householdName':'OIDC Smoke','timezone':'UTC'},{'Origin':origin,'X-Tendo-Setup-Token':os.environ['TENDO_SETUP_TOKEN']})
assert status==201,('setup',status,b)
status,h,b,_=request('POST','/api/v1/session',{'login':'oidc_owner','password':'correct horse battery'},{'Origin':origin})
assert status==201,('local login',status,b)
old_session=session_cookie(h)
status,h,b,_=request('GET','/api/v1/auth/oidc')
assert status==200 and b=={'enabled':True,'displayName':'OIDC Smoke Provider'},('status',status,b)
status,h,b,_=request('GET','/api/v1/auth/oidc/identity',headers={'Cookie':old_session})
assert status==200 and b=={'linked':False},('identity before link',status,b)
status,h,b,_=start('link','wrong password')
assert status==401 and b['code']=='invalid_credentials',('bad link password',status,b)
status,h,b,_,_=auth_start('link',old_session,sub='owner-subject',password='correct horse battery')
assert status==303 and h['Location']=='/account#oidc=connected' and not b and h['Cache-Control']=='no-store' and h['Referrer-Policy']=='no-referrer',('link callback',status,dict(h),b)
flow_clears=[x for x in h.get_all('Set-Cookie') if x.startswith('tendo_oidc=')]
assert len(flow_clears)==1 and 'Max-Age=0' in flow_clears[0],flow_clears
new_session=session_cookie(h)
status,_,b,_=request('GET','/api/v1/session',headers={'Cookie':old_session}); assert status==401,('old session should be revoked',status,b)
status,_,b,_=request('GET','/api/v1/session',headers={'Cookie':new_session}); assert status==200 and b['login']=='oidc_owner',('new link session',status,b)
status,_,b,_=request('GET','/api/v1/auth/oidc/identity',headers={'Cookie':new_session}); assert status==200 and b=={'linked':True},('identity linked',status,b)
status,logout_h,logout_b,_=request('DELETE','/api/v1/session',headers={'Origin':origin,'Cookie':new_session})
assert status==204 and logout_h.get('Cache-Control')=='no-store' and not logout_b,('logout',status,dict(logout_h),logout_b)
status,h,b,login_callback,login_flow=auth_start('login',sub='owner-subject')
assert status==303 and h['Location']=='/',('oidc login callback',status,h.get('Location'))
login_session=session_cookie(h)
status,_,session,_=request('GET','/api/v1/session',headers={'Cookie':login_session}); assert status==200 and session['login']=='oidc_owner',('oidc login session',status,session)
household=session['defaultHouseholdId']
status,_,hb,_=request('GET','/api/v1/households/'+household,headers={'Cookie':login_session}); assert status==200 and hb['name']=='OIDC Smoke',('OIDC household read',status,hb)
status,h,b,_=request('GET',login_callback,headers={'Cookie':login_flow}); assert status==303 and h['Location']=='/login#oidcError=invalid_flow' and not b,('callback replay',status,h.get('Location'))
def dbcounts():
 command=os.environ['OIDC_SMOKE_PSQL'].split()+['-c',"SELECT (SELECT count(*) FROM user_accounts)||'|'||(SELECT count(*) FROM household_memberships)||'|'||(SELECT count(*) FROM user_sessions)"]
 return subprocess.check_output(command,text=True).strip().splitlines()[-1]
before_counts=dbcounts()
status,h,b,_,_=auth_start('login',sub='unknown-subject'); assert status==303 and h['Location']=='/login#oidcError=identity_not_linked',('unknown identity',status,h.get('Location'))
after_counts=dbcounts(); assert before_counts==after_counts,('unknown identity changed admission tables',before_counts,after_counts)
status,h,b,_,_=auth_start('login',deny=True); assert status==303 and h['Location']=='/login#oidcError=cancelled' and not b,('provider denied',status,h.get('Location'))
status,h,b,_=start('login',origin_header='http://foreign.invalid'); assert status==403,('foreign Origin',status,b)
for fixture in fixtures:
 if fixture['status']==303:
  assert fixture['headers'].get('cache-control')=='no-store' and fixture['headers'].get('referrer-policy')=='no-referrer',fixture
  assert 'body' not in fixture,fixture
with open(os.environ['OIDC_SMOKE_FIXTURES'],'w',encoding='utf-8') as f: json.dump(fixtures,f)
PY
node "$root/api/check-health.mjs" "$fixtures"
"${compose[@]}" logs --no-color app >"$logs"
while IFS= read -r secret; do
  [[ -z "$secret" ]] && continue
  if grep -F "$secret" "$logs" >/dev/null; then echo 'OIDC code, state, or cookie value appeared in app logs' >&2; exit 1; fi
done <"$secrets"
if grep -E 'code=' "$logs" >/dev/null; then echo 'OIDC callback query marker found in app logs' >&2; exit 1; fi
printf 'OIDC API, PostgreSQL, provider, replay, denial, and log-privacy smoke passed.\n'
