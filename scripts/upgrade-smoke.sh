#!/usr/bin/env bash
# Prove a published release installation upgrades in place to this checkout without losing data.
# TENDO_UPGRADE_FROM selects the published source release (default v1.0.0); CI runs v1.0.0, v2.1.0 and v3.0.0.
set -Eeuo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
suffix="${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
project="tendo-upgrade-${suffix}"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-upgrade.XXXXXX")
export POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_OIDC_ISSUER TENDO_OIDC_CLIENT_ID TENDO_OIDC_CLIENT_SECRET TENDO_OIDC_CLIENT_SECRET_FILE TENDO_OIDC_DISPLAY_NAME TENDO_VERSION
TENDO_OIDC_ISSUER= TENDO_OIDC_CLIENT_ID= TENDO_OIDC_CLIENT_SECRET= TENDO_OIDC_CLIENT_SECRET_FILE= TENDO_OIDC_DISPLAY_NAME= TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no TENDO_DB_TIMEOUT=2 TENDO_SHUTDOWN_TIMEOUT=10
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
origin=$TENDO_PUBLIC_URL
compose=(docker compose --project-name "$project" -f "$root/compose.yaml")
compose_build=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml")

cleanup() {
  local result=$?
  trap - EXIT
  if ! docker compose --project-name "$project" -f "$root/compose.yaml" -f "$root/deploy/compose.build.yaml" down --volumes --remove-orphans --rmi local; then
    printf 'Compose cleanup failed for project %s\n' "$project" >&2
    (( result != 0 )) || result=1
  fi
  if ! rm -rf -- "$temp_dir"; then
    printf 'Could not remove private smoke directory\n' >&2
    (( result != 0 )) || result=1
  fi
  exit "$result"
}
trap cleanup EXIT

wait_ready() {
  local _
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 3 "$origin/health/ready" >/dev/null; then return 0; fi
    sleep 1
  done
  printf 'App did not become ready: %s/health/ready\n' "$origin" >&2
  return 1
}

migration_max() {
  find "$root/backend/internal/platform/database" -maxdepth 1 -type f -regextype posix-extended -regex '.*/[0-9]+_.*[.]sql' -printf '%f\n' | sed -E 's/^([0-9]+)_.*/\1/' | sort -n | tail -1
}
# Last migration shipped by each published source release (asserted against its live ledger below). CI
# checkouts are shallow and tagless, so the expectations are pinned here rather than read from tags.
source_version=${TENDO_UPGRADE_FROM:-v1.0.0}
TENDO_VERSION=$source_version
case "$TENDO_VERSION" in
  v1.0.0) expected_source=14 ;;
  v2.1.0) expected_source=16 ;;
  v3.0.0) expected_source=16 ;;
  *) printf 'Unsupported TENDO_UPGRADE_FROM=%s (supported: v1.0.0, v2.1.0, v3.0.0)\n' "$TENDO_VERSION" >&2; exit 2 ;;
esac
# Start the source release from the compose.yaml published with that exact release, not this checkout.
source_compose="$temp_dir/source-compose.yaml"
curl -fsSL -o "$source_compose" "https://github.com/bigtcze/tendo/releases/download/${TENDO_VERSION}/compose.yaml"
grep -qx 'services:' "$source_compose" || { printf 'Published %s compose.yaml is not a Compose file\n' "$TENDO_VERSION" >&2; exit 1; }
compose_source=(docker compose --project-name "$project" -f "$source_compose")
expected_current=$(migration_max)
(( 10#$expected_current >= expected_source )) || { printf 'Checkout migrations (%s) are older than %s (%s)\n' "$expected_current" "$TENDO_VERSION" "$expected_source" >&2; exit 1; }

# Row counts for every application table; ledger versions are checked separately.
snapshot_counts() {
  "$@" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At <<'SQL'
SELECT table_name || '=' || (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', table_schema, table_name), false, true, '')))[1]::text
FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'tendo_schema_migrations' ORDER BY table_name;
SQL
}
snapshot_ledger() {
  "$@" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT version::text || ' dirty=' || dirty::text FROM tendo_schema_migrations ORDER BY version"
}
api() {
  UPGRADE_SMOKE_ORIGIN="$origin" UPGRADE_SMOKE_STATE="$temp_dir/state.json" python3 "$temp_dir/api.py" "$1"
}
cat >"$temp_dir/api.py" <<'PY'
import json, os, sys, urllib.error, urllib.parse, urllib.request
origin=os.environ['UPGRADE_SMOKE_ORIGIN']; state_path=os.environ['UPGRADE_SMOKE_STATE']; phase=sys.argv[1]
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None
opener=urllib.request.build_opener(NoRedirect)
def req(method,path,body=None,cookie=None,headers=None):
    path=urllib.parse.quote(path,safe='/:?=&%')
    h=dict(headers or {})
    if cookie: h['Cookie']=f'tendo_session={cookie}'
    if method!='GET': h['Origin']=origin
    data=json.dumps(body,ensure_ascii=False).encode() if body is not None else None
    if data is not None: h['Content-Type']='application/json'
    request=urllib.request.Request(origin+path,data=data,headers=h,method=method)
    try:
        with opener.open(request,timeout=10) as r: status,hs,raw=r.status,r.headers,r.read()
    except urllib.error.HTTPError as e: status,hs,raw=e.code,e.headers,e.read()
    return status,hs,(json.loads(raw) if raw else None)
def ok(expected,method,path,body=None,cookie=None,headers=None):
    status,hs,b=req(method,path,body,cookie,headers); assert status==expected,(method,path,status,b); return hs,b
def login(name,password):
    hs,b=ok(201,'POST','/api/v1/session',{'login':name,'password':password}); token=hs['Set-Cookie'].split(';',1)[0].partition('=')[2]; assert len(token)==43; return token,b
def pages(path,cookie):
    out=[]; cursor=None
    while True:
        p=path if 'limit=' in path else path+('&' if '?' in path else '?')+'limit=2'
        if cursor: p+=('&' if '?' in p else '?')+'cursor='+cursor
        hs,b=ok(200,'GET',p,cookie=cookie); out.append({'path':p,'etag':hs.get('ETag'),'body':b}); cursor=b['nextCursor']
        if not cursor:return out
def capture(s):
    c=s['ownerSession']; base=f'/api/v1/households/{s["householdId"]}'; hs,b=ok(200,'GET',base,cookie=c)
    snap={'household':{'etag':hs.get('ETag'),'body':b},'members':pages(base+'/members?limit=1',c)}
    for archived in ('false','true'): snap[f'subjects archived={archived}']=pages(f'{base}/subjects?archived={archived}&limit=2',c)
    ids=set()
    for done in ('false','true'):
      for archived in ('false','true'):
        key=f'items done={done} archived={archived}'; snap[key]=pages(f'{base}/items?done={done}&archived={archived}&limit=2',c)
        ids.update(i['id'] for p in snap[key] for i in p['body']['items'])
    assert ids==set(s['items'].values()),('walked item IDs differ',len(ids),len(s['items']))
    for name,iid in sorted(s['items'].items()):
        hs,b=ok(200,'GET',f'{base}/items/{iid}',cookie=c); snap[f'item {name}']={'etag':hs.get('ETag'),'body':b}
        snap[f'completions {name}']=pages(f'{base}/items/{iid}/completions?limit=1',c)
    return snap
def populate():
    op=os.urandom(18).hex(); mp=os.urandom(18).hex()
    ok(201,'PUT','/api/v1/auth/setup',{'login':'owner_upgrade','password':op,'householdName':'Novákovi 家族','timezone':'Europe/Prague'},headers={'X-Tendo-Setup-Token':os.environ['TENDO_SETUP_TOKEN']})
    owner,session=login('owner_upgrade',op); hid=session['defaultHouseholdId']; base=f'/api/v1/households/{hid}'
    _,person=ok(201,'POST',base+'/subjects',{'name':'Výměna filtru Škoda','type':'person'},owner,{'Idempotency-Key':'upgrade-person'})
    _,car=ok(201,'POST',base+'/subjects',{'name':'Octavia','type':'vehicle'},owner,{'Idempotency-Key':'upgrade-car'})
    _,arch=ok(201,'POST',base+'/subjects',{'name':'Archived subject','type':'home'},owner,{'Idempotency-Key':'upgrade-archived-subject'})
    ok(200,'PATCH',f'{base}/subjects/{arch["id"]}',{'archived':True},owner,{'If-Match':'"1"'})
    _,invite=ok(201,'POST',base+'/invitations',{},owner,{'Idempotency-Key':'upgrade-member'})
    ok(201,'POST','/api/v1/auth/invitations/accept',{'token':invite['token'],'login':'member_upgrade','password':mp})
    _,members=ok(200,'GET',base+'/members',cookie=owner); member=next(m['userId'] for m in members['items'] if m['login']=='member_upgrade')
    # V1 supports descriptive person-to-member linkage; do it after member admission.
    ph,_=ok(200,'GET',f'{base}/subjects/{person["id"]}',cookie=owner)
    _,person=ok(200,'PATCH',f'{base}/subjects/{person["id"]}',{'linkedUserId':member},owner,{'If-Match':ph['ETag']})
    items={}; keys={}
    def create(name,body):
        key='upgrade-item-'+name; _,b=ok(201,'POST',base+'/items',body,owner,{'Idempotency-Key':key}); items[name]=b['id']; keys[name]=key; return b
    def complete(name,version,key,date): return ok(201,'POST',f'{base}/items/{items[name]}/completions',{'completedOn':date},owner,{'If-Match':f'"{version}"','Idempotency-Key':key})[1]
    create('unicode_literal',{'title':'Výměna filtru Škoda 100%_\\','notes':'Literal wildcard characters stay literal.','subjectId':person['id'],'responsibleUserId':member})
    create('percent_literal',{'title':'MARK%X literal','subjectId':person['id']})
    create('percent_decoy',{'title':'MARKAX decoy','subjectId':person['id']})
    create('underscore_literal',{'title':'UNDER_SCORE literal','subjectId':person['id']})
    create('underscore_decoy',{'title':'UNDERXSCORE decoy','subjectId':person['id']})
    create('backslash_literal',{'title':'BACK\\SLASH literal','subjectId':person['id']})
    create('backslash_decoy',{'title':'BACKSLASH decoy','subjectId':person['id']})
    create('oneoff',{'title':'Book dental check','subjectId':person['id'],'notes':'Ask about winter slot','attentionOn':'2025-03-01','responsibleUserId':member})
    fixed=create('fixed_historical',{'title':'Replace water filter','subjectId':person['id'],'historicalCompletedOn':'2024-01-31','recurrence':{'intervalValue':1,'intervalUnit':'month','mode':'fixed'}})
    assert fixed['attentionOn']=='2024-02-29',fixed
    create('fluid',{'title':'Water the plants','subjectId':person['id'],'attentionOn':'2025-01-01','recurrence':{'intervalValue':1,'intervalUnit':'week','mode':'after_completion'}})
    receipt=complete('fluid',1,'upgrade-replay-key','2025-01-05')
    create('yearly',{'title':'Car inspection','subjectId':car['id'],'attentionOn':'2020-03-01','recurrence':{'intervalValue':1,'intervalUnit':'year','mode':'fixed'}})
    complete('yearly',1,'upgrade-yearly-one','2025-02-01'); second=complete('yearly',2,'upgrade-yearly-two','2025-02-02')
    ok(200,'PATCH',f'{base}/items/{items["yearly"]}/completions/{second["id"]}',{'undone':True},owner,{'If-Match':'"3"'})
    create('done_oneoff',{'title':'Buy winter tyres','subjectId':car['id']}); complete('done_oneoff',1,'upgrade-done','2025-02-03')
    create('archived_item',{'title':'Paint the fence','subjectId':person['id'],'attentionOn':'2031-05-01'})
    ok(200,'PATCH',f'{base}/items/{items["archived_item"]}',{'archived':True},owner,{'If-Match':'"1"'})
    for n in range(60): create(f'page_{n:02d}',{'title':f'Pagination household obligation {n:02d}','subjectId':person['id']})
    page_zero=items['page_00']; page_zero_etag,_=ok(200,'GET',f'{base}/items/{page_zero}',cookie=owner)
    ok(200,'PATCH',f'{base}/items/{page_zero}',{'workflowState':'waiting'},owner,{'If-Match':page_zero_etag['ETag']})
    _,pending=ok(201,'POST',base+'/invitations',{},owner,{'Idempotency-Key':'upgrade-pending'})
    s={'ownerSession':owner,'ownerPassword':op,'memberPassword':mp,'householdId':hid,'personId':person['id'],'memberId':member,'items':items,'itemKeys':keys,'fluidReceipt':receipt,'pendingToken':pending['token'],'v1Session':owner}
    s['before']=capture(s)
    with open(state_path,'w',encoding='utf-8') as f: json.dump(s,f,ensure_ascii=False)
    print(f'Populated source release with {len(items)} items; captured public API representations.')
def verify():
    with open(state_path,encoding='utf-8') as f:s=json.load(f)
    after=capture(s); assert after.keys()==s['before'].keys()
    expected=json.loads(json.dumps(s['before']))
    # The admin-role migration changes only household_memberships.role; the members
    # API reports that column, so normalize the pre-upgrade snapshot there alone.
    for page in expected.get('members',[]):
        for item in page['body']['items']:
            if item.get('role')=='owner': item['role']='admin'
    # V4-07 adds the caller's viewerRole to the household body without a version bump;
    # the snapshot is read with the upgraded owner's session, so only that field may appear.
    if 'household' in expected:
        assert 'viewerRole' not in expected['household']['body'],('source release already exposed viewerRole',expected['household'])
        expected['household']['body']['viewerRole']='admin'
    def flatten(groups):
        return [item for page in groups for item in page['body']['items']]
    active_key='subjects archived=false'
    expected_active=flatten(expected[active_key]); actual_active=flatten(after[active_key])
    expected_ids={item['id'] for item in expected_active}
    preexisting=[item for item in actual_active if item['id'] in expected_ids]
    assert preexisting==expected_active,('pre-existing subject representations changed',expected_active,actual_active)
    members=flatten(after['members'])
    linked=({item.get('linkedUserId') for item in expected_active if item.get('linkedUserId')})
    missing={m['userId']:m['login'] for m in members if m['userId'] not in linked}
    added=[item for item in actual_active if item['id'] not in expected_ids]
    assert len(added)==len(missing),('backfilled member person count',missing,added)
    for item in added:
        assert item.get('type')=='person' and item.get('archived') is False and item.get('linkedUserId') in missing and item.get('name')==missing[item['linkedUserId']],('backfilled member person representation',item,missing)
    for k in expected:
        if k==active_key: continue
        if k=='subjects archived=true':
            assert flatten(after[k])==flatten(expected[k]),('archived subjects changed',expected[k],after[k])
        else:
            assert after[k]==expected[k],('source-release API snapshot differs (body or ETag)',k,expected[k],after[k])
    print(f'{len(after)} source-release API response groups preserved; {len(added)} canonical member persons added exactly.')
def exercise():
    with open(state_path,encoding='utf-8') as f:s=json.load(f)
    owner=s['v1Session']; base=f'/api/v1/households/{s["householdId"]}'
    ok(200,'GET',base,cookie=owner)
    member,_=login('member_upgrade',s['memberPassword']); assert member
    owner,_=login('owner_upgrade',s['ownerPassword'])
    partitions={}
    for done in ('false','true'):
      for archived in ('false','true'):
        path=f'{base}/items?done={done}&archived={archived}&limit=100'; walked=[]; cursor=None
        while True:
          p=path+('&cursor='+cursor if cursor else ''); _,page=ok(200,'GET',p,cookie=owner); walked.extend(page['items']); cursor=page['nextCursor']
          if not cursor:break
        partitions[(done,archived)]=walked
    summary=ok(200,'GET',f'{base}/items/summary',cookie=owner)[1]
    assert summary['total']==sum(map(len,partitions.values())),(summary,{k:len(v) for k,v in partitions.items()})
    assert summary['archived']==len(partitions[('false','true')])+len(partitions[('true','true')]),summary
    assert summary['done']==len(partitions[('true','false')]),summary
    assert sum(summary[k] for k in ('archived','done','paused','inProgress','waiting','needsAttention','upcoming'))==summary['total'],summary
    for query in ('vymena','SKODA'):
        result=ok(200,'GET',f'{base}/items?q={query}',cookie=owner)[1]['items']
        expected=s['items']['unicode_literal']; assert [i['id'] for i in result]==[expected],(query,[i['id'] for i in result])
    literal_queries=(('MARK%25X', 'percent_literal'),('UNDER_SCORE','underscore_literal'),('BACK%5CSLASH','backslash_literal'))
    for query,name in literal_queries:
        result=ok(200,'GET',f'{base}/items?q={query}',cookie=owner)[1]['items']
        expected=s['items'][name]; assert [i['id'] for i in result]==[expected],(query,name,[i['id'] for i in result])
    # Attention order is NULLS FIRST, then ascending attentionOn, then ascending id.
    expected_attention=sorted((i for i in partitions[('false','false')] if i['workflowState']=='open'),key=lambda i:(i['attentionOn'] is not None,i['attentionOn'] or '',i['id']))
    expected_ids=[i['id'] for i in expected_attention]; seen=[]; cursor=None
    while True:
        path=f'{base}/items?sort=attention_on&workflowState=open&limit=11'+('&cursor='+cursor if cursor else '')
        _,page=ok(200,'GET',path,cookie=owner); seen.extend(i['id'] for i in page['items']); cursor=page['nextCursor']
        if not cursor:break
    assert seen==expected_ids,('attention sort/filter ordered IDs differ',expected_ids,seen)
    iid=s['items']['fluid']; before=s['before']['item fluid']
    _,replay=ok(201,'POST',f'{base}/items/{iid}/completions',{'completedOn':'2025-01-05'},owner,{'If-Match':'"1"','Idempotency-Key':'upgrade-replay-key'})
    assert replay==s['fluidReceipt'],(replay,s['fluidReceipt'])
    fluid_hs,_=ok(200,'GET',f'{base}/items/{iid}',cookie=owner)
    assert fluid_hs['ETag']==before['etag'],('completion replay changed the item version',before['etag'],fluid_hs['ETag'])
    hs,undone=ok(200,'PATCH',f'{base}/items/{iid}/completions/{replay["id"]}',{'undone':True},owner,{'If-Match':fluid_hs['ETag']})
    assert undone['undoneAt'] is not None
    _,created=ok(201,'POST',base+'/items',{'title':'New V2 search record','subjectId':s['personId']},owner,{'Idempotency-Key':'upgrade-new-v2-item'})
    found=ok(200,'GET',f'{base}/items?q=V2%20search',cookie=owner)[1]['items']; assert [i['id'] for i in found]==[created['id']],found
    _,oldmembers=ok(200,'GET',base+'/members',cookie=owner)
    ok(201,'POST','/api/v1/auth/invitations/accept',{'token':s['pendingToken'],'login':'late_upgrade','password':os.urandom(18).hex()})
    _,newmembers=ok(200,'GET',base+'/members',cookie=owner); assert len(newmembers['items'])==len(oldmembers['items'])+1
    print('Summaries, search, attention pagination, source-release session, password login, completion replay/undo, create/search, and pending invitation passed.')
{'populate':populate,'verify':verify,'exercise':exercise}[phase]()
PY

POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
source_image="ghcr.io/bigtcze/tendo:${TENDO_VERSION}"
docker pull "$source_image"
pulled_image_id=$(docker image inspect "$source_image" --format '{{.Id}}')
"${compose_source[@]}" up -d --wait --wait-timeout 120
wait_ready
app_container=$("${compose_source[@]}" ps -q app)
[[ -n "$app_container" ]]
configured_image=$(docker inspect "$app_container" --format '{{.Config.Image}}')
running_image_id=$(docker inspect "$app_container" --format '{{.Image}}')
[[ "$configured_image" == "$source_image" ]] || { printf 'Source app configured image is %s, expected exactly %s\n' "$configured_image" "$source_image" >&2; exit 1; }
[[ "$running_image_id" == "$pulled_image_id" ]] || { printf 'Source app image ID %s differs from pulled image ID %s\n' "$running_image_id" "$pulled_image_id" >&2; exit 1; }
ledger=$("${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT max(version)::text || ':' || bool_or(dirty)::text FROM tendo_schema_migrations")
[[ "$ledger" == "${expected_source}:false" ]] || { printf 'Published %s schema unexpected: %s\n' "$TENDO_VERSION" "$ledger" >&2; exit 1; }
api populate
# Migration 018 provisions one canonical person for every membership without a
# linked person and unlinks non-canonical duplicates. Compute the exact
# expected deltas from the source schema before upgrading.
has_linked_column=$("${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='subjects' AND column_name='linked_user_id')")
if [[ "$has_linked_column" == "t" ]]; then
  expected_backfill=$("${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM household_memberships m WHERE NOT EXISTS (SELECT 1 FROM subjects s WHERE s.household_id=m.household_id AND s.linked_user_id=m.user_id)")
  expected_unlinks=$("${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM (SELECT row_number() OVER (PARTITION BY household_id, linked_user_id ORDER BY created_at, id) AS position FROM subjects WHERE linked_user_id IS NOT NULL) ranked WHERE position > 1")
else
  expected_backfill=$("${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM household_memberships")
  expected_unlinks=0
fi
"${compose_source[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT id::text FROM subjects ORDER BY id" >"$temp_dir/subject-ids-before.txt"
"${compose_source[@]}" stop app
snapshot_counts "${compose_source[@]}" >"$temp_dir/counts-before.txt"
snapshot_ledger "${compose_source[@]}" >"$temp_dir/ledger-before.txt"

# Reuse the same Postgres service/volume. The checkout app bootstraps the role and schema
# itself; --remove-orphans deletes the source release's one-shot migrate container.
unset TENDO_VERSION
"${compose_build[@]}" up -d --build --wait --wait-timeout 240 --remove-orphans
wait_ready
ledger=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT max(version)::text || ':' || bool_or(dirty)::text FROM tendo_schema_migrations")
[[ "$ledger" == "$((10#$expected_current)):false" ]] || { printf 'Upgrade ledger unexpected: %s (expected %s)\n' "$ledger" "$((10#$expected_current)):false" >&2; exit 1; }
# The stored role password was not changed by the upgrade: the restricted login still works.
role_user=$("${compose_build[@]}" exec -T -e "TENDO_DATABASE_PASSWORD=$TENDO_DATABASE_PASSWORD" postgres sh -c 'PGPASSWORD=$TENDO_DATABASE_PASSWORD psql -h 127.0.0.1 -U tendo -d tendo -At -c "SELECT current_user"')
[[ "$role_user" == tendo ]] || { printf 'Upgraded restricted role login returned %s\n' "$role_user" >&2; exit 1; }
[[ -z $(docker ps -aq --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=migrate") ]] \
  || { printf 'Source release migrate container survived the upgrade\n' >&2; exit 1; }
snapshot_counts "${compose_build[@]}" >"$temp_dir/counts-after.txt"
subjects_after=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM subjects")
unlinks_after=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM member_person_migration_unlinks")
memberships_without_single=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM household_memberships m WHERE (SELECT count(*) FROM subjects s WHERE s.household_id=m.household_id AND s.linked_user_id=m.user_id AND s.type='person') <> 1")
"${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT id::text FROM subjects ORDER BY id" >"$temp_dir/subject-ids-after.txt"
accounts_with_email=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM user_accounts WHERE email IS NOT NULL OR email_verified_at IS NOT NULL")
ACCOUNTS_WITH_EMAIL="$accounts_with_email" EXPECTED_BACKFILL="$expected_backfill" EXPECTED_UNLINKS="$expected_unlinks" SUBJECTS_AFTER="$subjects_after" UNLINKS_AFTER="$unlinks_after" MEMBERSHIPS_WITHOUT_SINGLE="$memberships_without_single" \
  python3 - "$temp_dir/counts-before.txt" "$temp_dir/counts-after.txt" "$temp_dir/subject-ids-before.txt" "$temp_dir/subject-ids-after.txt" <<'PY'
import os
import sys
def load(path):
    values = {}
    for line in open(path, encoding='utf-8'):
        name, separator, count = line.strip().partition('=')
        if separator:
            values[name] = count
    return values
before = load(sys.argv[1])
after = load(sys.argv[2])
expected_backfill = int(os.environ['EXPECTED_BACKFILL'])
expected_unlinks = int(os.environ['EXPECTED_UNLINKS'])
assert int(os.environ['SUBJECTS_AFTER']) == int(before['subjects']) + expected_backfill, ('subjects count after upgrade', before['subjects'], expected_backfill, os.environ['SUBJECTS_AFTER'])
for name, count in before.items():
    if name == 'subjects':
        continue
    assert after.get(name) == count, ('pre-existing table row counts changed during upgrade', name, count, after.get(name))
unexpected = set(after) - set(before) - {'household_role_changes', 'member_person_migration_unlinks', 'oidc_pending_logins'}
assert not unexpected, ('unexpected new application tables during upgrade', sorted(unexpected))
assert after.get('oidc_pending_logins') == '0', ('migration 019 pending continuation table must be empty after upgrade', after.get('oidc_pending_logins'))
assert os.environ['ACCOUNTS_WITH_EMAIL'] == '0', ('migration 019 must never infer an email for pre-existing accounts', os.environ['ACCOUNTS_WITH_EMAIL'])
assert 'household_role_changes' in after, ('migration 017 audit table missing after upgrade', sorted(after))
assert after['household_role_changes'] == '0', ('new table must be empty after migration', 'household_role_changes', after['household_role_changes'])
assert 'member_person_migration_unlinks' in after, ('migration 018 audit table missing after upgrade', sorted(after))
assert int(os.environ['UNLINKS_AFTER']) == expected_unlinks, ('member person unlink log count', expected_unlinks, os.environ['UNLINKS_AFTER'])
assert os.environ['MEMBERSHIPS_WITHOUT_SINGLE'] == '0', ('membership without exactly one linked person', os.environ['MEMBERSHIPS_WITHOUT_SINGLE'])
before_ids = set(open(sys.argv[3], encoding='utf-8').read().split())
after_ids = set(open(sys.argv[4], encoding='utf-8').read().split())
missing = sorted(before_ids - after_ids)
assert not missing, ('pre-existing subject ids disappeared during upgrade', missing[:10])
print(f'{len(before)} pre-existing table row counts preserved; subjects +{expected_backfill} canonical member persons; {expected_unlinks} duplicate links logged; household_role_changes empty.')
PY
snapshot_ledger "${compose_build[@]}" >"$temp_dir/ledger-after.txt"
python3 - "$temp_dir/ledger-before.txt" "$temp_dir/ledger-after.txt" <<'PY'
import sys
before = open(sys.argv[1], encoding='utf-8').read().splitlines()
after = open(sys.argv[2], encoding='utf-8').read().splitlines()
assert after[:len(before)] == before, ('pre-existing migration ledger entries changed', before, after)
assert all(line.endswith('dirty=false') for line in after), ('dirty migration ledger entry', after)
print(f'{len(before)} pre-existing migration ledger entries preserved; {len(after) - len(before)} migrations applied cleanly.')
PY
api verify
api exercise
printf 'Published %s in-place upgrade smoke passed.\n' "$source_version"
