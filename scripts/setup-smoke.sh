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
session_fixtures=$(mktemp)
item_fixtures=$(mktemp)
cleanup() {
  local result=$?
  trap - EXIT
  if ! "${compose[@]}" down --volumes --remove-orphans; then
    printf 'Compose cleanup failed\n' >&2
    ((result != 0)) || result=1
  fi
  if ! rm -f "$fixtures" "$session_fixtures" "$item_fixtures"; then
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
item_fixtures=[]
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
psql_command="${compose[*]} exec -T postgres psql -U postgres -d tendo -v ON_ERROR_STOP=1 -At"
SETUP_SMOKE_ORIGIN="$origin" SETUP_SMOKE_FIXTURES="$session_fixtures" SETUP_SMOKE_ITEM_FIXTURES="$item_fixtures" SETUP_SMOKE_PSQL="$psql_command" python3 - <<'PY'
import base64,hashlib,json,os,shlex,subprocess,urllib.request,urllib.error
origin=os.environ['SETUP_SMOKE_ORIGIN']
fixtures=[]
item_fixtures=[]
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
# Item API smoke: persistence, derived attention, ETags, archive, and boundaries.
ICOLL='/api/v1/households/{householdId}/items'
IITEM='/api/v1/households/{householdId}/items/{itemId}'
CITEM='/api/v1/households/{householdId}/items/{itemId}/completions'
def ireq(method,url,template,body=None,headers=None,record=True):
 h={**(headers or {})};data=None
 if body is not None:h['Content-Type']='application/json';data=json.dumps(body,ensure_ascii=False).encode()
 r=urllib.request.Request(origin+url,data=data,headers=h,method=method)
 try:
  with opener.open(r,timeout=8) as x:status,hs,raw=x.status,x.headers,x.read()
 except urllib.error.HTTPError as e:status,hs,raw=e.code,e.headers,e.read()
 result=json.loads(raw) if raw else None
 if record:item_fixtures.append({'path':template,'method':method,'status':status,'headers':{k:hs.get_all(k)[0] for k in hs.keys()},'body':result})
 return status,hs,result
ibase=f'/api/v1/households/{household}/items'
status,h,immediate=ireq('POST',ibase,ICOLL,{'title':'  Připomenout pojištění 🚗  ','subjectId':person['id'],'notes':'Poznámka 家族'},mut)
assert status==201 and immediate['title']=='Připomenout pojištění 🚗' and immediate['attention']=='needs_attention' and h['ETag']=='"1"',('create immediate item',status,immediate)
assert psql(f"SELECT title||'|'||notes||'|'||workflow_state||'|'||archived||'|'||version FROM items WHERE id='{immediate['id']}' AND household_id='{household}'")== 'Připomenout pojištění 🚗|Poznámka 家族|open|false|1','persisted item row'
status,h,future=ireq('POST',ibase,ICOLL,{'title':'Future item','subjectId':person['id'],'attentionOn':'2099-12-31'},mut)
assert status==201 and future['attention']=='upcoming' and future['recurrence'] is None,('future attention',status,future)
status,h,fixed=ireq('POST',ibase,ICOLL,{'title':'Fixed yearly','subjectId':person['id'],'attentionOn':'2030-01-15','recurrence':{'intervalValue':1,'intervalUnit':'year','mode':'fixed'}},mut)
assert status==201 and fixed['recurrence']=={'intervalValue':1,'intervalUnit':'year','mode':'fixed'},('fixed recurrence',status,fixed)
status,h,fluid=ireq('POST',ibase,ICOLL,{'title':'Fluid monthly','subjectId':person['id'],'attentionOn':'2030-02-20','recurrence':{'intervalValue':1,'intervalUnit':'month','mode':'after_completion'}},mut)
assert status==201 and fluid['recurrence']=={'intervalValue':1,'intervalUnit':'month','mode':'after_completion'},('fluid recurrence',status,fluid)
for rec_item, expected in ((fixed, '1|year|fixed'),(fluid,'1|month|after_completion')):
 row=psql(f"SELECT recurrence_interval_value||'|'||recurrence_interval_unit||'|'||recurrence_mode||'|'||attention_on::text FROM items WHERE id='{rec_item['id']}'")
 assert row==expected+'|'+rec_item['attentionOn'],('persisted recurrence',row,expected)
 rec_path=ibase+'/'+rec_item['id']
 status,h,cleared=ireq('PATCH',rec_path,IITEM,{'recurrence':None},{**mut,'If-Match':'"1"'})
 assert status==200 and cleared['recurrence'] is None and cleared['attentionOn']==rec_item['attentionOn'] and h['ETag']=='"2"',('clear recurrence',status,cleared)
 assert psql(f"SELECT COALESCE(recurrence_interval_value::text||'|'||recurrence_interval_unit||'|'||recurrence_mode,'NULL')||'|'||attention_on::text FROM items WHERE id='{rec_item['id']}'")== 'NULL|'+rec_item['attentionOn'],'recurrence clear modified cycle state'
 status,h,restored=ireq('PATCH',rec_path,IITEM,{'recurrence':rec_item['recurrence']},{**mut,'If-Match':'"2"'})
 assert status==200 and restored['recurrence']==rec_item['recurrence'] and restored['attentionOn']==rec_item['attentionOn'] and h['ETag']=='"3"',('restore recurrence',status,restored)
 assert psql(f"SELECT recurrence_interval_value||'|'||recurrence_interval_unit||'|'||recurrence_mode||'|'||attention_on::text FROM items WHERE id='{rec_item['id']}'")==expected+'|'+rec_item['attentionOn'],'recurrence restore changed cycle state'
ipath=ibase+'/'+immediate['id']
status,h,got=ireq('GET',ipath,IITEM,headers=cookie_header)
assert status==200 and got==immediate and h['ETag']=='"1"',('item get',status,got)
status,h,fixed_get=ireq('GET',ibase+'/'+fixed['id'],IITEM,headers=cookie_header)
assert status==200 and fixed_get['recurrence']=={'intervalValue':1,'intervalUnit':'year','mode':'fixed'} and fixed_get['attentionOn']=='2030-01-15',('fixed item get',status,fixed_get)
status,h,patched=ireq('PATCH',ipath,IITEM,{'title':'Insurance follow-up','workflowState':'in_progress','attentionOn':None},{**mut,'If-Match':'"1"'})
assert status==200 and patched['title']=='Insurance follow-up' and patched['workflowState']=='in_progress' and patched['attentionOn'] is None and patched['attention']=='needs_attention' and h['ETag']=='"2"',('item patch',status,patched)
assert psql(f"SELECT title||'|'||workflow_state||'|'||COALESCE(attention_on::text,'NULL')||'|'||version FROM items WHERE id='{immediate['id']}'")== 'Insurance follow-up|in_progress|NULL|2','item patch persisted'
status,_,b=ireq('PATCH',ipath,IITEM,{'title':'missing precondition'},mut);assert status==428 and b['code']=='precondition_required',('item missing If-Match',status,b)
status,_,b=ireq('PATCH',ipath,IITEM,{'title':'stale'},{**mut,'If-Match':'"1"'});assert status==412 and b['code']=='precondition_failed',('item stale If-Match',status,b)
status,h,archived=ireq('PATCH',ipath,IITEM,{'archived':True},{**mut,'If-Match':'"2"'});assert status==200 and archived['archived'] is True and h['ETag']=='"3"',('item archive',status,archived)
status,_,active_items=ireq('GET',ibase,ICOLL,headers=cookie_header);assert status==200 and [x['id'] for x in active_items['items']]==[future['id'],fixed['id'],fluid['id']],('active items',status,active_items)
active_by_id={x['id']:x for x in active_items['items']}
assert active_by_id[fixed['id']]['recurrence']==fixed['recurrence'] and active_by_id[fixed['id']]['attentionOn']==fixed['attentionOn'],('fixed list recurrence/attention',active_by_id[fixed['id']])
assert active_by_id[fluid['id']]['recurrence']==fluid['recurrence'] and active_by_id[fluid['id']]['attentionOn']==fluid['attentionOn'],('fluid list recurrence/attention',active_by_id[fluid['id']])
status,_,archived_items=ireq('GET',ibase+'?archived=true',ICOLL,headers=cookie_header);assert status==200 and [x['id'] for x in archived_items['items']]==[immediate['id']],('archived items',status,archived_items)
status,_,b=ireq('POST',ibase,ICOLL,{'title':'Foreign subject','subjectId':foreign},mut);assert status==422 and b['field']=='subjectId' and b['code']=='invalid_reference',('foreign subject reference',status,b)
# Historical create, history and a validation problem are captured against the OpenAPI schemas.
historical_policy={'intervalValue':1,'intervalUnit':'month','mode':'fixed'}
status,h,historical=ireq('POST',ibase,ICOLL,{'title':'Smoke historical create','subjectId':person['id'],'historicalCompletedOn':'2025-10-31','recurrence':historical_policy},mut)
assert status==201 and h['ETag']=='"2"' and historical['attentionOn']=='2025-11-30' and historical['lastCompletedOn']=='2025-10-31',('historical item create',status,h,historical)
historical_completions=f'{ibase}/{historical["id"]}/completions'
status,h,historical_history=ireq('GET',historical_completions,'/api/v1/households/{householdId}/items/{itemId}/completions',headers=cookie_header)
assert status==200 and len(historical_history['items'])==1 and historical_history['items'][0]['completedOn']=='2025-10-31' and historical_history['items'][0]['cycleAttentionOn'] is None and historical_history['items'][0]['nextAttentionOn']=='2025-11-30',('historical receipt history',status,historical_history)
status,h,historical_invalid=ireq('POST',ibase,ICOLL,{'title':'Smoke invalid historical create','subjectId':person['id'],'historicalCompletedOn':'2025-10-31'},mut)
assert status==422 and historical_invalid['field']=='historicalCompletedOn' and historical_invalid['code']=='requires_recurrence',('historical validation',status,historical_invalid)

# Completion vertical slice: fixed/fluid cadence, one-off lifecycle and receipt idempotency.
cpath=lambda x:ibase+'/'+x+'/completions'
fixed_late=ireq('POST',ibase,ICOLL,{'title':'Completion fixed late','subjectId':person['id'],'attentionOn':'2026-09-01','recurrence':{'intervalValue':1,'intervalUnit':'year','mode':'fixed'}},mut)[2]
status,_,receipt=ireq('POST',cpath(fixed_late['id']),CITEM,{'completedOn':'2026-10-06'},{**mut,'If-Match':'"1"','Idempotency-Key':'fixed-late'})
assert status==201 and receipt['completedOn']=='2026-10-06' and receipt['nextAttentionOn']=='2027-09-01',('fixed completion',status,receipt)
assert psql(f"SELECT attention_on::text||'|'||version||'|'||done FROM items WHERE id='{fixed_late['id']}'")== '2027-09-01|2|false','fixed completion persistence'
fixed_undo_path=cpath(fixed_late['id'])+'/'+receipt['id']
CUNDO='/api/v1/households/{householdId}/items/{itemId}/completions/{completionId}'
CITEM_PATCH='/api/v1/households/{householdId}/items/{itemId}'
status,_,undone=ireq('PATCH',fixed_undo_path,CUNDO,{'undone':True},{**mut,'If-Match':'\"2\"'})
assert status==200 and undone['undoneAt'] and undone['undoneByUserId'] and undone['id']==receipt['id'],('undo receipt',status,undone)
assert psql(f"SELECT attention_on::text||'|'||version||'|'||done FROM items WHERE id='{fixed_late['id']}'")== '2026-09-01|3|false' and psql(f"SELECT undone_at IS NOT NULL FROM item_completions WHERE id='{receipt['id']}'")== 't','undo restored state and receipt'
status,_,item_after_undo=ireq('GET',ibase+'/'+fixed_late['id'],IITEM,headers=cookie_header);assert status==200 and item_after_undo['lastCompletedOn'] is None,('undo lastCompletedOn',status,item_after_undo)
status,_,undo_replay=ireq('PATCH',fixed_undo_path,CUNDO,{'undone':True},{**mut,'If-Match':'\"1\"'},record=False);assert status==200 and undo_replay['id']==undone['id'] and undo_replay['undoneAt']==undone['undoneAt'] and psql(f"SELECT version::text FROM items WHERE id='{fixed_late['id']}'")== '3',('undo replay',status,undo_replay)
status,_,active_one=ireq('POST',cpath(fixed_late['id']),CITEM,{'completedOn':'2026-10-06'},{**mut,'If-Match':'\"3\"','Idempotency-Key':'active-one'});assert status==201,('first active retry completion',status,active_one)
status,_,active_two=ireq('POST',cpath(fixed_late['id']),CITEM,{'completedOn':'2026-10-06'},{**mut,'If-Match':'\"4\"','Idempotency-Key':'active-two'});assert status==201,('second active completion',status,active_two)
status,_,not_latest=ireq('PATCH',cpath(fixed_late['id'])+'/'+active_one['id'],CUNDO,{'undone':True},{**mut,'If-Match':'\"5\"'});assert status==409 and not_latest['code']=='completion_not_latest',('not-latest undo',status,not_latest)
fluid=ireq('POST',ibase,ICOLL,{'title':'Completion fluid late','subjectId':person['id'],'attentionOn':'2026-09-01','recurrence':{'intervalValue':12,'intervalUnit':'month','mode':'after_completion'}},mut)[2]
status,_,fluid_receipt=ireq('POST',cpath(fluid['id']),CITEM,{'completedOn':'2026-10-06'},{**mut,'If-Match':'"1"','Idempotency-Key':'fluid-late'})
assert status==201 and fluid_receipt['nextAttentionOn']=='2027-10-06' and psql(f"SELECT attention_on::text FROM items WHERE id='{fluid['id']}'")== '2027-10-06',('fluid completion',status,fluid_receipt)
oneoff=ireq('POST',ibase,ICOLL,{'title':'Completion one off','subjectId':person['id']},mut)[2];complete_headers={**mut,'If-Match':'"1"','Idempotency-Key':'oneoff-key'}
status,ch,one_receipt=ireq('POST',cpath(oneoff['id']),CITEM,{},complete_headers);assert status==201 and 'ETag' not in ch and one_receipt['itemId']==oneoff['id'],('one-off receipt',status,one_receipt)
stored_version=psql(f"SELECT version::text FROM items WHERE id='{oneoff['id']}'");status,_,replay=ireq('POST',cpath(oneoff['id']),CITEM,{},complete_headers);assert status==201 and replay==one_receipt and psql(f"SELECT version::text FROM items WHERE id='{oneoff['id']}'")==stored_version,('idempotent replay',status,replay)
status,_,b=ireq('POST',cpath(oneoff['id']),CITEM,{'completedOn':one_receipt['completedOn']},{**complete_headers,'If-Match':'"2"'});assert status==422 and b['code']=='idempotency_key_reused',('key reused',status,b)
status,_,b=ireq('POST',cpath(oneoff['id']),CITEM,{}, {**mut,'Idempotency-Key':'missing-match'});assert status==428 and b['code']=='precondition_required',('completion missing If-Match',status,b)
status,_,b=ireq('POST',cpath(oneoff['id']),CITEM,{}, {**mut,'If-Match':'"1"'});assert status==400 and b['code']=='idempotency_key_required',('completion missing key',status,b)
status,_,b=ireq('POST',cpath(oneoff['id']),CITEM,{}, {**mut,'If-Match':'"1"','Idempotency-Key':'stale'});assert status==412 and b['code']=='precondition_failed',('completion stale etag',status,b)
status,_,b=ireq('POST',cpath(oneoff['id']),CITEM,{}, {**mut,'If-Match':'"2"','Idempotency-Key':'archived'});assert status==409 and b['code']=='item_done',('done conflict',status,b)
assert psql(f"SELECT done||'|'||version FROM items WHERE id='{oneoff['id']}'")== 'true|2','one-off done state'
status,_,default_items=ireq('GET',ibase,ICOLL,headers=cookie_header);assert oneoff['id'] not in [x['id'] for x in default_items['items']],('done item in default list',default_items)
status,_,done_items=ireq('GET',ibase+'?done=true',ICOLL,headers=cookie_header);assert any(x['id']==oneoff['id'] and x['done'] is True for x in done_items['items']),('done list',done_items)
status,_,bad_done=ireq('GET',ibase+'?done=bad',ICOLL,headers=cookie_header);assert status==400 and bad_done['code']=='invalid_query' and bad_done['parameter']=='done',('invalid done filter',status,bad_done)
status,_,history=ireq('GET',cpath(oneoff['id']),CITEM,headers=cookie_header);assert status==200 and len(history['items'])==1 and history['items'][0]==one_receipt,('completion history',status,history)
status,_,b=ireq('GET',f'/api/v1/households/{other_id}/items/{oneoff["id"]}/completions',CITEM,headers=cookie_header);assert status==404 and b['code']=='not_found',('foreign item completion list',status,b)
status,_,b=ireq('GET',f'/api/v1/households/{other_id}/items',ICOLL,headers=cookie_header);assert status==404 and b['code']=='not_found',('non-member items',status,b)
status,_,b=ireq('GET',ibase,ICOLL);assert status==401 and b['code']=='unauthenticated',('anonymous items',status,b)
status,_,b=ireq('POST',ibase,ICOLL,{'title':'Origin blocked','subjectId':person['id']},{**cookie_header,'Origin':'http://foreign.example'});assert status==403,('foreign origin items',status,b)
assert psql(f"SELECT count(*) FROM items WHERE household_id='{household}'")== '8' and psql(f"SELECT title||'|'||workflow_state||'|'||version FROM items WHERE id='{immediate['id']}'")== 'Insurance follow-up|in_progress|3','item rejected request or archive persistence'
# Invitation API production-image smoke. The owner cookie remains valid through this block.
INV='/api/v1/households/{householdId}/invitations'
INV_ITEM='/api/v1/households/{householdId}/invitations/{invitationId}'
ACCEPT_NEW='/api/v1/auth/invitations/accept'
ACCEPT_EXISTING='/api/v1/invitations/accept'
MEMBERS='/api/v1/households/{householdId}/members'
def xreq(method,url,template,body=None,headers=None,record=True):
 hs={**(headers or {})};data=None
 if body is not None: hs['Content-Type']='application/json';data=json.dumps(body,ensure_ascii=False).encode()
 request=urllib.request.Request(origin+url,data=data,headers=hs,method=method)
 try:
  with opener.open(request,timeout=8) as response: code,rh,raw=response.status,response.headers,response.read()
 except urllib.error.HTTPError as error: code,rh,raw=error.code,error.headers,error.read()
 result=json.loads(raw) if raw else None
 if record: fixtures.append({'path':template,'method':method,'status':code,'headers':{k:rh.get_all(k)[0] for k in rh.keys()},**({'body':result} if raw else {})})
 return code,rh,result,raw

def record_wrong_host():
 request=urllib.request.Request(origin+'/api/v1/households/'+household+'/invitations',headers={**cookie_header,'Host':'attacker.invalid'},method='GET')
 try:
  with opener.open(request,timeout=8) as response: code,rh,raw=response.status,response.headers,response.read()
 except urllib.error.HTTPError as error: code,rh,raw=error.code,error.headers,error.read()
 result=json.loads(raw) if raw else None
 fixtures.append({'path':INV,'method':'get','status':code,'headers':{k:rh.get_all(k)[0] for k in rh.keys()},'body':result})
 assert code==421,('invitation wrong authority',code,result)
owner_mut={**cookie_header,'Origin':origin}
owner_cookie=cookie_header
INV='/api/v1/households/{householdId}/invitations'
INV_ITEM='/api/v1/households/{householdId}/invitations/{invitationId}'
ACCEPT_NEW='/api/v1/auth/invitations/accept'
ACCEPT_EXISTING='/api/v1/invitations/accept'
MEMBERS='/api/v1/households/{householdId}/members'
invite_base=f'/api/v1/households/{household}/invitations'
record_wrong_host()
st,ih,created,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-invite-1'})
assert st==201 and isinstance(created.get('token'),str) and len(created['token'])==43,('invitation create',st,created)
invitation_token=created['token']; invitation_id=created['id']
st,_,retry,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-invite-1'})
assert st==200 and retry['id']==invitation_id and 'token' not in retry,('idempotency retry',st,retry)
st,_,missing_key,_,=xreq('POST',invite_base,INV,{},owner_mut)
assert st==400 and missing_key['code']=='idempotency_key_required',('missing invitation key',st,missing_key)
st,_,foreign_origin_problem,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Origin':'http://foreign.example','Idempotency-Key':'foreign-smoke-origin'})
assert st==403,('foreign origin invitation create',st,foreign_origin_problem)
invitation_digest=hashlib.sha256(invitation_token.encode()).hexdigest()
assert psql(f"SELECT count(*) FROM household_invitations WHERE id='{invitation_id}' AND token_hash=decode('{invitation_digest}','hex')")=='1','invitation token digest not stored'
assert psql(f"SELECT count(*) FROM household_invitations i WHERE i.id='{invitation_id}' AND to_jsonb(i)::text LIKE '%'||'{invitation_token}'||'%'")=='0','raw invitation token appears in database row'
st,_,listed,_=xreq('GET',invite_base+'?limit=10',INV,headers=cookie_header)
assert st==200 and any(item['id']==invitation_id and item['status']=='pending' for item in listed['items']) and all('token' not in item for item in listed['items']),('pending invitation list',st,listed)
st,_,accepted,_=xreq('POST',ACCEPT_NEW,ACCEPT_NEW,{'token':invitation_token,'login':'member_smoke','password':'a sufficiently long member passphrase'},{'Origin':origin})
assert st==201 and accepted['login']=='member_smoke' and accepted['role']=='member',('new account accept',st,accepted)
member_login={'login':'member_smoke','password':'a sufficiently long member passphrase'}
st,mh,member_session,_=req('POST',member_login,{'Origin':origin})
assert st==201 and member_session['login']=='member_smoke','new member login failed'
member_token=mh['Set-Cookie'].split(';',1)[0]
member_cookie={'Cookie':member_token}
st,_,owner_members,_=xreq('GET',f'/api/v1/households/{household}/members',MEMBERS,headers=cookie_header)
assert st==200 and {(m['login'],m['role']) for m in owner_members['items']}=={('owner_smoke','owner'),('member_smoke','member')},('owner member list',st,owner_members)
st,_,joined_members,_=xreq('GET',f'/api/v1/households/{household}/members',MEMBERS,headers=member_cookie)
assert st==200 and {(m['login'],m['role']) for m in joined_members['items']}=={('owner_smoke','owner'),('member_smoke','member')},('member member list',st,joined_members)
assert psql(f"SELECT m.role||'|'||(u.default_household_id=m.household_id)::text FROM household_memberships m JOIN user_accounts u ON u.id=m.user_id WHERE u.login='member_smoke' AND m.household_id='{household}'")=='member|true','membership/default household persistence'
st,_,reuse,_=xreq('POST',ACCEPT_NEW,ACCEPT_NEW,{'token':invitation_token,'login':'second_smoke','password':'a sufficiently long second passphrase'},{'Origin':origin})
assert st==404 and reuse['code']=='invalid_invitation',('reused invitation',st,reuse)
st,_,malformed,_=xreq('POST',ACCEPT_NEW,ACCEPT_NEW,{'token':'malformed','login':'second_smoke','password':'a sufficiently long second passphrase'},{'Origin':origin})
assert st==404 and malformed==reuse,('malformed token must be indistinguishable',st,malformed,reuse)
st,_,forbidden,_=xreq('POST',invite_base,INV,{}, {**member_cookie,'Origin':origin,'Idempotency-Key':'member-must-not-invite'})
assert st==403 and forbidden['code']=='owner_required',('member create invitation',st,forbidden)
st,_,member_list,_=xreq('GET',invite_base,INV,headers=member_cookie)
assert st==403 and member_list['code']=='owner_required',('member list invitations',st,member_list)
st,_,member_revoke_problem,_=xreq('DELETE',f'/api/v1/households/{household}/invitations/{invitation_id}',INV_ITEM,headers={**member_cookie,'Origin':origin})
assert st==403 and member_revoke_problem['code']=='owner_required',('member revoke invitation',st,member_revoke_problem)
st,_,second,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-invite-revoke'})
assert st==201 and len(second['token'])==43,('second invitation',st,second)
revoke_path=f'/api/v1/households/{household}/invitations/{second["id"]}'
st,_,_,_=xreq('DELETE',revoke_path,INV_ITEM,headers=owner_mut)
assert st==204,('revoke invitation',st)
st,_,revoked_accept,_=xreq('POST',ACCEPT_NEW,ACCEPT_NEW,{'token':second['token'],'login':'revoked_smoke','password':'a sufficiently long revoked passphrase'},{'Origin':origin})
assert st==404 and revoked_accept['code']=='invalid_invitation',('accept revoked invitation',st,revoked_accept)
st,_,member_conflict_invite,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-member-conflict'})
assert st==201,('member conflict invitation',st,member_conflict_invite)
st,_,new_member_conflict,_=xreq('POST',ACCEPT_EXISTING,ACCEPT_EXISTING,{'token':member_conflict_invite['token']}, {**member_cookie,'Origin':origin})
assert st==409 and new_member_conflict['code']=='already_member',('existing member acceptance conflict',st,new_member_conflict)
assert psql(f"SELECT accepted_at IS NULL FROM household_invitations WHERE id='{member_conflict_invite['id']}'")=='t','already member conflict consumed invitation'
# Skip 429 fixture: the limiter shares client-IP budget with the remaining invitation checks.
st,_,_,_=xreq('DELETE',revoke_path,INV_ITEM,headers=owner_mut)
assert st==204,('revoke idempotency',st)
st,_,nonmember_invites,_=xreq('GET',f'/api/v1/households/{other_id}/invitations',INV,headers=cookie_header)
assert st==404 and nonmember_invites['code']=='not_found',('nonmember invitations',st,nonmember_invites)
st,_,nonmember_members,_=xreq('GET',f'/api/v1/households/{other_id}/members',MEMBERS,headers=cookie_header)
assert st==404 and nonmember_members['code']=='not_found',('nonmember members',st,nonmember_members)
st,_,anonymous_invites,_=xreq('GET',invite_base,INV)
assert st==401 and anonymous_invites['code']=='unauthenticated',('anonymous invitations',st,anonymous_invites)
st,_,foreign_origin,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Origin':'http://foreign.example','Idempotency-Key':'foreign-origin'})
assert st==403,('foreign Origin create invitation',st,foreign_origin)
st,_,expired,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-invite-expiry'})
assert st==201 and len(expired['token'])==43,('expiry invitation create',st,expired)
psql(f"UPDATE household_invitations SET created_at=now()-interval '8 days', expires_at=now()-interval '1 second' WHERE id='{expired['id']}'")
assert psql(f"SELECT expires_at<now() FROM household_invitations WHERE id='{expired['id']}'")=='t','could not expire invitation fixture'
st,_,expired_accept,_=xreq('POST',ACCEPT_NEW,ACCEPT_NEW,{'token':expired['token'],'login':'expired_smoke','password':'a sufficiently long expired passphrase'},{'Origin':origin})
assert st==404 and expired_accept['code']=='invalid_invitation',('expired invitation',st,expired_accept)
# The owner already belongs to this household; acceptance must be refused without consuming a fresh invitation.
st,_,own_invite,_=xreq('POST',invite_base,INV,{}, {**owner_mut,'Idempotency-Key':'smoke-owner-conflict'})
assert st==201,('owner conflict invitation',st,own_invite)
st,_,owner_conflict,_=xreq('POST',ACCEPT_EXISTING,ACCEPT_EXISTING,{'token':own_invite['token']},owner_mut)
assert st==409 and owner_conflict['code']=='already_member',('owner already belongs to target household',st,owner_conflict)
assert psql(f"SELECT accepted_at IS NULL AND revoked_at IS NULL FROM household_invitations WHERE id='{own_invite['id']}'")=='t','already-member failure consumed invitation'
status,h,_,_=req('DELETE',headers={'Origin':origin,'Cookie':f'tendo_session={token}'})
assert status==204,('logout',status)
st,stale_headers,stale_problem,_=xreq('GET',invite_base,INV,headers={'Cookie':f'tendo_session={token}'})
assert st==401 and stale_problem['code']=='unauthenticated' and 'Set-Cookie' in stale_headers,('stale session invitation 401',st,stale_problem,dict(stale_headers))
cleared=[p.strip().lower() for p in h['Set-Cookie'].split(';')]
assert cleared[0]=='tendo_session=' and 'max-age=0' in cleared and 'httponly' in cleared and 'samesite=lax' in cleared and 'path=/' in cleared,cleared
status,_,_,_=req('DELETE',headers={'Cookie':f'tendo_session={token}'},record=False);assert status==403,('logout without Origin',status)
status,_,b,_=req('GET',headers={'Cookie':f'tendo_session={token}'});assert status==401 and b['code']=='unauthenticated',('revoked cookie',status,b)
status,_,b=ireq('PATCH',fixed_undo_path,CUNDO,{'undone':True},{'Origin':origin,'Cookie':f'tendo_session={token}','If-Match':'"3"'});assert status==401 and b['code']=='unauthenticated',('undo with revoked cookie',status,b)
status,h,b_revoked=hreq(household,cookie_header);assert status==401 and b_revoked['code']=='unauthenticated',('household with revoked cookie',status,b_revoked)
login_headers={'Origin':origin,'Content-Type':'application/json'}
login_req=urllib.request.Request(origin+'/api/v1/session',data=json.dumps({'login':'owner_smoke','password':'correct horse battery'}).encode(),headers=login_headers,method='POST')
with opener.open(login_req,timeout=8) as login_resp: login_cookie=login_resp.headers.get_all('Set-Cookie')[0].split(';',1)[0]
new_token=login_cookie.split('=',1)[1]
replay_version=psql(f"SELECT version::text FROM items WHERE id='{fixed_late['id']}'")
status,_,undone_retry=ireq('POST',cpath(fixed_late['id']),CITEM,{'completedOn':'2026-10-06'},{'Origin':origin,'Cookie':login_cookie,'If-Match':'"3"','Idempotency-Key':'fixed-late'});assert status==201 and undone_retry['id']==receipt['id'] and undone_retry['undoneAt']==undone['undoneAt'] and undone_retry['undoneByUserId']==undone['undoneByUserId'] and psql(f"SELECT version::text FROM items WHERE id='{fixed_late['id']}'")== replay_version,('undone completion-key replay',status,undone_retry)
stale=[p.strip().lower() for p in h['Set-Cookie'].split(';')]
assert stale[0]=='tendo_session=' and 'max-age=0' in stale and 'httponly' in stale and 'samesite=lax' in stale and 'path=/' in stale,stale
assert psql(f"SELECT count(*) FROM user_sessions WHERE token_hash=decode('{__import__('hashlib').sha256(new_token.encode()).hexdigest()}','hex')")== '1','new session row should remain'
with open(os.environ['SETUP_SMOKE_FIXTURES'],'w',encoding='utf-8') as f:json.dump(fixtures,f)
with open(os.environ['SETUP_SMOKE_ITEM_FIXTURES'],'w',encoding='utf-8') as f:json.dump(item_fixtures,f)
PY
API_RESPONSE_FIXTURES="$session_fixtures" node "$root/api/check-health.mjs"
API_RESPONSE_FIXTURES="$fixtures" node "$root/api/check-health.mjs"
API_RESPONSE_FIXTURES="$item_fixtures" node "$root/api/check-health.mjs"
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
