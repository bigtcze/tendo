#!/usr/bin/env bash
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="tendo-setup-smoke-${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
export COMPOSE_PROJECT_NAME=$project POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
compose=(docker compose --project-name "$project" -f "$root/compose.yaml")
fixtures=$(mktemp)
cleanup() {
  local result=$?
  trap - EXIT
  if ! "${compose[@]}" down --volumes --remove-orphans; then
    printf 'Compose cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  if ! rm -f "$fixtures"; then
    printf 'API fixture cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT
origin=$TENDO_PUBLIC_URL
"${compose[@]}" up -d --build --wait --wait-timeout 120
ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null; then ready=1; break; fi
  sleep 1
done
if (( ready == 0 )); then printf 'App did not become ready: %s/health/ready\n' "$origin" >&2; exit 1; fi
SETUP_SMOKE_ORIGIN="$origin" SETUP_SMOKE_FIXTURES="$fixtures" python3 - <<'PY'
import json,os,sys,urllib.request,urllib.error
origin=os.environ['SETUP_SMOKE_ORIGIN']
token=os.environ['TENDO_SETUP_TOKEN']
path=os.environ['SETUP_SMOKE_FIXTURES']
fixtures=[]
def req(method,url,body=None,headers=None):
 h={'Content-Type':'application/json',**(headers or {})}
 data=body if isinstance(body,bytes) else json.dumps(body,ensure_ascii=False).encode() if body is not None else None
 r=urllib.request.Request(origin+url,data=data,headers=h,method=method)
 try:
  with urllib.request.urlopen(r,timeout=8) as x: status,hs,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e: status,hs,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 if url=='/api/v1/auth/setup' and method=='PUT' and status != 409:
  fixtures.append({'path':url,'method':method,'status':status,'headers':dict(hs.items()),'body':result})
 return status,hs,result
status,h,b=req('GET','/api/v1/auth/setup');assert status==200 and b=={'required':True} and h['ETag']=='"setup-v1-required"'
status,_,_=req('GET','/api/v1/auth/setup',headers={'Origin':'http://foreign.example'});assert status==403,('foreign origin',status)
status,_,_=req('GET','/api/v1/auth/setup',headers={'Host':'attacker.example'});assert status==421,('wrong authority',status)
valid={'login':'owner_smoke','password':'correct horse battery','householdName':'Veselí 家族','timezone':'Europe/Prague'}
status,_,_=req('PUT','/api/v1/auth/setup',valid,{'Origin':origin,'X-Tendo-Setup-Token':'wrong'});assert status==401,('wrong token',status)
status,_,_=req('PUT','/api/v1/auth/setup',valid,{'X-Tendo-Setup-Token':token});assert status==403,('missing origin',status)
status,_,_=req('PUT','/api/v1/auth/setup',b'x'*8193,{'Origin':origin,'X-Tendo-Setup-Token':token});assert status==413,('size',status)
status,_,_=req('PUT','/api/v1/auth/setup',valid,{'Origin':'http://foreign.example','X-Tendo-Setup-Token':token});assert status==403,('foreign origin',status)
status,_,b=req('GET','/api/v1/auth/setup');assert status==200 and b=={'required':True},('state changed before setup',status,b)
status,_,_=req('PUT','/api/v1/auth/setup',b'{}',{'Origin':origin,'X-Tendo-Setup-Token':token,'Content-Type':'text/plain'});assert status==415,('media',status)
invalid={**valid,'timezone':'Not/A_Timezone'}
status,_,_=req('PUT','/api/v1/auth/setup',invalid,{'Origin':origin,'X-Tendo-Setup-Token':token});assert status==422,('validation',status)
status,_,b=req('GET','/api/v1/auth/setup');assert status==200 and b=={'required':True},('still required',status,b)
status,h,b=req('PUT','/api/v1/auth/setup',valid,{'Origin':origin,'X-Tendo-Setup-Token':token});assert status==201 and b=={'required':False},('create',status,b,dict(h))
fixtures.append({'path':'/api/v1/auth/setup','method':'PUT','status':status,'headers':dict(h.items()),'body':b})
status,_,b=req('GET','/api/v1/auth/setup');assert status==200 and b=={'required':False},('after setup',status,b)
status,_,_=req('PUT','/api/v1/auth/setup',valid,{'Origin':origin,'X-Tendo-Setup-Token':token});assert status==429,('duplicate rate limit',status)
with open(path,'w',encoding='utf-8') as f:json.dump(fixtures,f,ensure_ascii=False)
PY
API_RESPONSE_FIXTURES="$fixtures" node "$root/api/check-health.mjs"
"${compose[@]}" exec -T postgres psql -U postgres -d tendo -v ON_ERROR_STOP=1 -c "DO \$\$ BEGIN IF (SELECT count(*) FROM user_accounts)=1 AND (SELECT count(*) FROM households)=1 AND (SELECT count(*) FROM household_memberships)=1 AND EXISTS (SELECT 1 FROM user_accounts u JOIN local_credentials c ON c.user_id=u.id JOIN household_memberships m ON m.user_id=u.id JOIN households h ON h.id=m.household_id WHERE u.login='owner_smoke' AND h.name='Veselí 家族' AND h.timezone='Europe/Prague' AND m.role='owner' AND u.default_household_id=h.id AND c.password_hash LIKE '\$argon2%') THEN RETURN; END IF; RAISE EXCEPTION 'first-owner state assertion failed'; END \$\$"
session_fixtures=$(mktemp)
psql_command="${compose[*]} exec -T postgres psql -U postgres -d tendo -v ON_ERROR_STOP=1 -At"
SETUP_SMOKE_ORIGIN="$origin" SETUP_SMOKE_FIXTURES="$session_fixtures" SETUP_SMOKE_PSQL="$psql_command" python3 - <<'PY'
import base64,hashlib,json,os,shlex,subprocess,urllib.request,urllib.error
origin=os.environ['SETUP_SMOKE_ORIGIN']
fixtures=[]
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k): return None
opener=urllib.request.build_opener(NoRedirect)
def req(method,body=None,headers=None,record=True):
 h={**(headers or {})}
 data=None
 if body is not None:
  h['Content-Type']='application/json'
  data=json.dumps(body).encode()
 r=urllib.request.Request(origin+'/api/v1/session',data=data,headers=h,method=method)
 try:
  with opener.open(r,timeout=8) as x: status,hs,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e: status,hs,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 if record:
  fixture={'path':'/api/v1/session','method':method,'status':status,'headers':{k:hs.get_all(k)[0] for k in hs.keys()}}
  if raw: fixture['body']=result
  fixtures.append(fixture)
 return status,hs,result,raw
def psql(sql):
 return subprocess.run(shlex.split(os.environ['SETUP_SMOKE_PSQL'])+['-c',sql],check=True,capture_output=True,text=True).stdout.strip()
login={'login':'owner_smoke','password':'correct horse battery'}
bad,h,b,_=req('POST',{**login,'password':'wrong password value'},{'Origin':origin})
assert bad==401 and b['code']=='invalid_credentials' and 'Set-Cookie' not in h,('wrong password',bad,b)
status,_,unknown,_=req('POST',{'login':'nobody_here','password':'correct horse battery'},{'Origin':origin})
assert status==401 and unknown==b,('unknown login must match wrong password body',status,unknown)
status,_,_,_=req('POST',login,record=False);assert status==403,('login without Origin',status)
status,_,_,_=req('POST',login,{'Origin':'http://foreign.example'},record=False);assert status==403,('login foreign Origin',status)
assert psql('SELECT count(*) FROM user_sessions')=='0','failed logins created sessions'
status,_,b,_=req('GET');assert status==401 and b['code']=='unauthenticated',('anonymous session',status,b)
status,h,b,raw=req('POST',login,{'Origin':origin})
assert status==201 and h['Location']=='/api/v1/session',('login',status,b)
cookie=h['Set-Cookie']
parts=[p.strip() for p in cookie.split(';')]
name,_,token=parts[0].partition('=')
attrs=[p.lower() for p in parts[1:]]
assert name=='tendo_session' and len(token)==43,('cookie name/token',name,len(token))
assert 'httponly' in attrs and 'samesite=lax' in attrs and 'path=/' in attrs and 'max-age=2592000' in attrs,attrs
assert 'secure' not in attrs and not any(a.startswith('domain=') for a in attrs),attrs
assert token.encode() not in raw,'token leaked in body'
household=psql("SELECT default_household_id FROM user_accounts WHERE login='owner_smoke'")
assert b['login']=='owner_smoke' and b['defaultHouseholdId']==household,(b,household)
status,h,b2,_=req('GET',headers={'Cookie':f'tendo_session={token}'})
assert status==200 and b2==b and h['ETag'].startswith('"session-'),('get session',status,b2,dict(h))
digest=hashlib.sha256(token.encode()).hexdigest()
raw_hex=base64.urlsafe_b64decode(token+'=').hex()
assert psql(f"SELECT count(*) FROM user_sessions WHERE token_hash=decode('{digest}','hex')")=='1','session digest not stored'
assert psql(f"SELECT count(*) FROM user_sessions WHERE token_hash=decode('{raw_hex}','hex') OR token_hash=convert_to('{token}','UTF8')")=='0','raw token stored'
cookie_header={'Cookie':f'tendo_session={token}'}
def hreq(hid,headers=None,record=True):
 r=urllib.request.Request(origin+'/api/v1/households/'+hid,headers=headers or {},method='GET')
 try:
  with opener.open(r,timeout=8) as x: status,hs,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e: status,hs,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 if record: fixtures.append({'path':'/api/v1/households/{householdId}','method':'GET','status':status,'headers':{k:hs.get_all(k)[0] for k in hs.keys()},'body':result})
 return status,hs,result
status,h,hb=hreq(household,cookie_header)
assert status==200 and set(hb)=={'id','name','timezone','createdAt'} and hb['id']==household and hb['name']=='Veselí 家族' and hb['timezone']=='Europe/Prague' and hb['createdAt'].endswith('Z'),('get household',status,hb)
assert h['ETag']=='"1"' and h['Cache-Control']=='no-store' and h['Content-Type'].startswith('application/json'),dict(h)
assert psql(f"SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS') FROM households WHERE id='{household}'")==hb['createdAt'].rstrip('Z').split('.')[0],('createdAt mismatch',hb)
status,h,b_anon=hreq(household);assert status==401 and b_anon['code']=='unauthenticated' and 'Set-Cookie' not in h,('household without cookie',status,b_anon)
random_id=psql('SELECT uuidv7()')
status,_,b_missing=hreq(random_id,cookie_header);assert status==404 and b_missing['code']=='not_found',('nonexistent household',status,b_missing)
status,_,b_bad=hreq('not-a-uuid',cookie_header);assert status==404 and b_bad==b_missing,('malformed household id',status,b_bad)
status,_,_=hreq(household,{**cookie_header,'Origin':'http://foreign.example'},record=False);assert status==403,('household foreign Origin',status)
other_id=psql("WITH x AS (INSERT INTO households(name,timezone) VALUES ('Other','UTC') RETURNING id) SELECT id FROM x")
assert other_id!=household and psql(f"SELECT count(*) FROM households WHERE id='{other_id}'")=='1' and psql(f"SELECT count(*) FROM household_memberships WHERE household_id='{other_id}'")=='0','other household fixture'
status,h,b_other=hreq(other_id,cookie_header);assert status==404 and b_other==b_missing and 'ETag' not in h,('non-member household must match nonexistent 404',status,b_other,b_missing)
COLL='/api/v1/households/{householdId}/subjects'
ITEM='/api/v1/households/{householdId}/subjects/{subjectId}'
def sreq(method,url,template,body=None,headers=None,record=True):
 h={**(headers or {})}
 data=None
 if body is not None:
  h['Content-Type']='application/json'
  data=json.dumps(body,ensure_ascii=False).encode()
 r=urllib.request.Request(origin+url,data=data,headers=h,method=method)
 try:
  with opener.open(r,timeout=8) as x: status,hs,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e: status,hs,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 if record: fixtures.append({'path':template,'method':method,'status':status,'headers':{k:hs.get_all(k)[0] for k in hs.keys()},'body':result})
 return status,hs,result
sbase=f'/api/v1/households/{household}/subjects'
mut={**cookie_header,'Origin':origin}
accounts_before=psql('SELECT count(*) FROM user_accounts')
status,h,person=sreq('POST',sbase,COLL,{'name':'  Babička Líba 👵  ','type':'person'},mut)
assert status==201 and set(person)=={'id','type','name','archived','createdAt','updatedAt'} and person['name']=='Babička Líba 👵' and person['type']=='person' and person['archived'] is False,('create person',status,person)
assert h['Location']==f"{sbase}/{person['id']}" and h['ETag']=='"1"' and h['Cache-Control']=='no-store',dict(h)
assert psql('SELECT count(*) FROM user_accounts')==accounts_before,'person subject must not create an account'
assert psql(f"SELECT name||'|'||type||'|'||version||'|'||archived FROM subjects WHERE id='{person['id']}' AND household_id='{household}'")=='Babička Líba 👵|person|1|false','stored subject'
status,h,car=sreq('POST',sbase,COLL,{'name':'Octavia','type':'vehicle'},mut)
assert status==201 and car['type']=='vehicle' and h['ETag']=='"1"',('create vehicle',status,car)
status,_,b=sreq('POST',sbase,COLL,{'name':'   ','type':'vehicle'},mut)
assert status==422 and b['field']=='name' and b['code']=='invalid_length',('blank name',status,b)
status,_,b=sreq('POST',sbase,COLL,{'name':'x','type':'robot'},mut)
assert status==422 and b['field']=='type' and b['code']=='invalid_type',('bad type',status,b)
status,_,b=sreq('POST',sbase,COLL,{'name':'x','type':'pet','extra':1},mut)
assert status==400 and b['code']=='invalid_request',('unknown key',status,b)
status,h,lst=sreq('GET',sbase,COLL,headers=cookie_header)
assert status==200 and [i['id'] for i in lst['items']]==[person['id'],car['id']] and lst['nextCursor'] is None,('list',status,lst)
status,h,p1=sreq('GET',sbase+'?limit=1',COLL,headers=cookie_header)
assert status==200 and [i['id'] for i in p1['items']]==[person['id']] and isinstance(p1['nextCursor'],str) and p1['nextCursor'],('page 1',status,p1)
status,h,p2=sreq('GET',sbase+'?limit=1&cursor='+p1['nextCursor'],COLL,headers=cookie_header)
assert status==200 and [i['id'] for i in p2['items']]==[car['id']] and p2['nextCursor'] is None,('page 2',status,p2)
status,_,b=sreq('GET',sbase+'?limit=0',COLL,headers=cookie_header);assert status==400 and b['code']=='invalid_query' and b['parameter']=='limit',('limit',status,b)
status,_,b=sreq('GET',sbase+'?cursor=%2A%2A',COLL,headers=cookie_header);assert status==400 and b['parameter']=='cursor',('cursor',status,b)
status,_,b=sreq('GET',sbase+'?archived=maybe',COLL,headers=cookie_header);assert status==400 and b['parameter']=='archived',('archived',status,b)
status,h,got=sreq('GET',sbase+'/'+car['id'],ITEM,headers=cookie_header)
assert status==200 and got==car and h['ETag']=='"1"',('get',status,got)
spath=sbase+'/'+car['id']
status,h,renamed=sreq('PATCH',spath,ITEM,{'name':'Škoda Octavia'},{**mut,'If-Match':'"1"'})
assert status==200 and renamed['name']=='Škoda Octavia' and renamed['id']==car['id'] and h['ETag']=='"2"',('rename',status,renamed,dict(h))
assert psql(f"SELECT name||'|'||version FROM subjects WHERE id='{car['id']}'")=='Škoda Octavia|2','rename not persisted'
status,_,b=sreq('PATCH',spath,ITEM,{'name':'No precondition'},mut)
assert status==428 and b['code']=='precondition_required',('no If-Match',status,b)
status,_,b=sreq('PATCH',spath,ITEM,{'name':'Stale'},{**mut,'If-Match':'"1"'})
assert status==412 and b['code']=='precondition_failed',('stale',status,b)
status,_,b=sreq('PATCH',spath,ITEM,{'name':'Weak'},{**mut,'If-Match':'W/"2"'})
assert status==412,('weak',status,b)
status,_,b=sreq('PATCH',spath,ITEM,{},{**mut,'If-Match':'"2"'})
assert status==400 and b['code']=='invalid_request',('empty patch',status,b)
assert psql(f"SELECT name||'|'||version FROM subjects WHERE id='{car['id']}'")=='Škoda Octavia|2','failed patches changed the row'
status,h,archived=sreq('PATCH',spath,ITEM,{'archived':True},{**mut,'If-Match':'"2"'})
assert status==200 and archived['archived'] is True and h['ETag']=='"3"',('archive',status,archived)
status,_,lst=sreq('GET',sbase,COLL,headers=cookie_header)
assert [i['id'] for i in lst['items']]==[person['id']],('default list must exclude archived',lst)
status,_,lst=sreq('GET',sbase+'?archived=true',COLL,headers=cookie_header)
assert [i['id'] for i in lst['items']]==[car['id']] and lst['nextCursor'] is None,('archived list',lst)
status,_,got=sreq('GET',spath,ITEM,headers=cookie_header);assert status==200 and got['archived'] is True,('archived get',status,got)
assert psql(f"SELECT archived||'|'||version FROM subjects WHERE id='{car['id']}'")=='true|3','archive not persisted'
status,h,back=sreq('PATCH',spath,ITEM,{'archived':False},{**mut,'If-Match':'"3"'});assert status==200 and back['archived'] is False and h['ETag']=='"4"',('unarchive',status,back)
foreign=psql(f"INSERT INTO subjects(household_id,type,name) VALUES ('{other_id}','home','Foreign') RETURNING id").splitlines()[0]
status,_,n1=sreq('GET',sbase+'/'+random_id,ITEM,headers=cookie_header);assert status==404 and n1['code']=='not_found',('unknown subject',status,n1)
status,_,n2=sreq('GET',sbase+'/not-a-uuid',ITEM,headers=cookie_header);assert status==404 and n2==n1,('malformed subject',status,n2)
status,h,n3=sreq('GET',sbase+'/'+foreign,ITEM,headers=cookie_header);assert status==404 and n3==n1 and 'ETag' not in h,('foreign subject via own household',status,n3)
status,_,n4=sreq('GET',f'/api/v1/households/{other_id}/subjects/{foreign}',ITEM,headers=cookie_header);assert status==404 and n4==n1,('non-member household subject',status,n4)
status,_,n5=sreq('GET',f'/api/v1/households/{other_id}/subjects',COLL,headers=cookie_header);assert status==404 and n5==n1,('non-member household list',status,n5)
status,_,n6=sreq('POST',f'/api/v1/households/{other_id}/subjects',COLL,{'name':'x','type':'home'},mut);assert status==404 and n6==n1,('non-member create',status,n6)
status,_,n7=sreq('PATCH',sbase+'/'+foreign,ITEM,{'name':'Hijack'},{**mut,'If-Match':'"1"'});assert status==404 and n7==n1,('foreign subject patch',status,n7)
status,_,n8=sreq('PATCH',f'/api/v1/households/{other_id}/subjects/{foreign}',ITEM,{'name':'Hijack'},{**mut,'If-Match':'"1"'});assert status==404 and n8==n1,('non-member household subject patch',status,n8)
assert psql(f"SELECT name||'|'||version FROM subjects WHERE id='{foreign}'")=='Foreign|1','foreign subject changed'
assert psql(f"SELECT count(*) FROM subjects WHERE household_id='{other_id}'")=='1','non-member create persisted'
status,_,b=sreq('GET',sbase,COLL);assert status==401 and b['code']=='unauthenticated',('anonymous list',status,b)
status,_,b=sreq('POST',sbase,COLL,{'name':'x','type':'home'},{'Origin':origin});assert status==401,('anonymous create',status,b)
status,_,b=sreq('POST',sbase,COLL,{'name':'x','type':'home'},{**cookie_header,'Origin':'http://foreign.example'});assert status==403,('foreign Origin POST',status,b)
status,_,b=sreq('POST',sbase,COLL,{'name':'x','type':'home'},cookie_header);assert status==403,('missing Origin POST',status,b)
status,_,b=sreq('PATCH',spath,ITEM,{'name':'x'},{**cookie_header,'Origin':'http://foreign.example','If-Match':'"4"'});assert status==403,('foreign Origin PATCH',status,b)
assert psql(f"SELECT count(*) FROM subjects WHERE household_id='{household}'")=='2' and psql(f"SELECT name||'|'||version FROM subjects WHERE id='{car['id']}'")=='Škoda Octavia|4','rejected requests changed subjects'
status,h,_,_=req('DELETE',headers={'Origin':origin,'Cookie':f'tendo_session={token}'})
assert status==204,('logout',status)
cleared=[p.strip().lower() for p in h['Set-Cookie'].split(';')]
assert cleared[0]=='tendo_session=' and 'max-age=0' in cleared and 'httponly' in cleared and 'samesite=lax' in cleared and 'path=/' in cleared,cleared
status,_,_,_=req('DELETE',headers={'Cookie':f'tendo_session={token}'},record=False);assert status==403,('logout without Origin',status)
status,_,b,_=req('GET',headers={'Cookie':f'tendo_session={token}'});assert status==401 and b['code']=='unauthenticated',('revoked cookie',status,b)
status,h,b_revoked=hreq(household,cookie_header);assert status==401 and b_revoked['code']=='unauthenticated',('household with revoked cookie',status,b_revoked)
stale=[p.strip().lower() for p in h['Set-Cookie'].split(';')]
assert stale[0]=='tendo_session=' and 'max-age=0' in stale and 'httponly' in stale and 'samesite=lax' in stale and 'path=/' in stale,stale
assert psql('SELECT count(*) FROM user_sessions')=='0','session row survived logout'
with open(os.environ['SETUP_SMOKE_FIXTURES'],'w',encoding='utf-8') as f:json.dump(fixtures,f)
PY
API_RESPONSE_FIXTURES="$session_fixtures" node "$root/api/check-health.mjs"
rm -f "$session_fixtures"
"${compose[@]}" restart app >/dev/null
persisted=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$origin/api/v1/auth/setup" | python3 -c 'import json,sys; assert json.load(sys.stdin)=={"required":False}' 2>/dev/null; then persisted=1; break; fi
  sleep 1
done
if (( persisted == 0 )); then printf 'First-owner setup state did not persist across app restart\n' >&2; exit 1; fi
SETUP_SMOKE_ORIGIN="$origin" python3 - <<'PY'
import json,os,urllib.request,urllib.error
origin=os.environ['SETUP_SMOKE_ORIGIN']
request=urllib.request.Request(origin+'/api/v1/auth/setup',data=json.dumps({'login':'owner_smoke','password':'correct horse battery','householdName':'Veselí 家族','timezone':'Europe/Prague'},ensure_ascii=False).encode(),headers={'Content-Type':'application/json','Origin':origin,'X-Tendo-Setup-Token':os.environ['TENDO_SETUP_TOKEN']},method='PUT')
try:
 urllib.request.urlopen(request,timeout=8)
 raise AssertionError('duplicate setup unexpectedly succeeded')
except urllib.error.HTTPError as error:
 assert error.code==409,('duplicate setup after restart',error.code)
PY
printf 'First-owner setup and local session API smoke passed.\n'
