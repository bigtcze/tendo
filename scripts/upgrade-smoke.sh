#!/usr/bin/env bash
# Prove a published release installation upgrades in place to this checkout without losing data.
# TENDO_UPGRADE_FROM selects the published source release (default v1.0.0); CI runs v1.0.0, v2.1.0, v3.0.0 and v4.0.0.
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
  v4.0.0) expected_source=20 ;;
  *) printf 'Unsupported TENDO_UPGRADE_FROM=%s (supported: v1.0.0, v2.1.0, v3.0.0, v4.0.0)\n' "$TENDO_VERSION" >&2; exit 2 ;;
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
# Recurrence data that migration 021 must add to without rewriting: item schedule state
# and every receipt's snapshot dates and undo state.
snapshot_recurrence() {
  "$@" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At <<'SQL'
SELECT 'item|' || id::text || '|' || coalesce(attention_on::text, '-') || '|' || version::text || '|' || done::text FROM items ORDER BY id;
SELECT 'receipt|' || item_id::text || '|' || item_version_before::text || '|' || coalesce(cycle_attention_on::text, '-') || '|' || completed_on::text || '|' || coalesce(next_attention_on::text, '-') || '|' || (undone_at IS NOT NULL)::text FROM item_completions ORDER BY item_id, item_version_before;
SQL
}
item_column() {
  "${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT $1 FROM items WHERE id='${recurrence_item[$2]}'"
}
api() {
  UPGRADE_SMOKE_ORIGIN="$origin" UPGRADE_SMOKE_STATE="$temp_dir/state.json" UPGRADE_SMOKE_RECURRENCE="$temp_dir/recurrence.tsv" UPGRADE_SMOKE_SOURCE="$source_version" python3 "$temp_dir/api.py" "$1"
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
    # V1-V3 only describe the person-to-member link, so link the fixture person after
    # admission. V4 provisions a canonical person for every member at setup and at
    # invitation acceptance and rejects a second link with 409 member_person_exists,
    # so there the member's own canonical person is the fixture person.
    listed=[item for page in pages(base+'/subjects?archived=false',owner) for item in page['body']['items']]
    canonical=next((item for item in listed if item.get('linkedUserId')==member),None)
    if canonical is not None:
        assert canonical.get('type')=='person' and canonical.get('archived') is False,canonical
        person=canonical
    else:
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
    # V5 anchor fixtures, completed on the source release so migration 021 has real
    # pre-V5 data: a Jan 31 fixed monthly item and a Feb 29 fixed yearly item both clamp
    # in the source release, which is exactly what the anchor recovery must read.
    create('anchor_exact',{'title':'Anchor exact monthly','subjectId':person['id'],'attentionOn':'2025-01-31','recurrence':{'intervalValue':1,'intervalUnit':'month','mode':'fixed'}})
    exact=complete('anchor_exact',1,'upgrade-anchor-exact','2025-01-31')
    assert exact['nextAttentionOn']=='2025-02-28',exact
    create('anchor_ambiguous',{'title':'Anchor ambiguous monthly','subjectId':person['id'],'attentionOn':'2025-01-31','recurrence':{'intervalValue':1,'intervalUnit':'month','mode':'fixed'}})
    ambiguous=complete('anchor_ambiguous',1,'upgrade-anchor-ambiguous','2025-01-31')
    assert ambiguous['nextAttentionOn']=='2025-02-28',ambiguous
    ambiguous_hs,_=ok(200,'GET',f'{base}/items/{items["anchor_ambiguous"]}',cookie=owner)
    assert ambiguous_hs['ETag']=='"2"',ambiguous_hs
    _,ambiguous_patched=ok(200,'PATCH',f'{base}/items/{items["anchor_ambiguous"]}',{'notes':'Edited after the completion, so the receipt chain is discontinuous.'},owner,{'If-Match':ambiguous_hs['ETag']})
    assert ambiguous_patched['attentionOn']=='2025-02-28',ambiguous_patched
    create('anchor_yearly',{'title':'Anchor yearly leap day','subjectId':car['id'],'attentionOn':'2024-02-29','recurrence':{'intervalValue':1,'intervalUnit':'year','mode':'fixed'}})
    yearly_anchor=complete('anchor_yearly',1,'upgrade-anchor-yearly','2024-02-29')
    assert yearly_anchor['nextAttentionOn']=='2025-02-28',yearly_anchor
    for n in range(60): create(f'page_{n:02d}',{'title':f'Pagination household obligation {n:02d}','subjectId':person['id']})
    page_zero=items['page_00']; page_zero_etag,_=ok(200,'GET',f'{base}/items/{page_zero}',cookie=owner)
    ok(200,'PATCH',f'{base}/items/{page_zero}',{'workflowState':'waiting'},owner,{'If-Match':page_zero_etag['ETag']})
    _,pending=ok(201,'POST',base+'/invitations',{},owner,{'Idempotency-Key':'upgrade-pending'})
    s={'ownerSession':owner,'ownerPassword':op,'memberPassword':mp,'householdId':hid,'personId':person['id'],'memberId':member,'items':items,'itemKeys':keys,'fluidReceipt':receipt,'pendingToken':pending['token'],'v1Session':owner}
    s['before']=capture(s)
    with open(state_path,'w',encoding='utf-8') as f: json.dump(s,f,ensure_ascii=False)
    with open(os.environ['UPGRADE_SMOKE_RECURRENCE'],'w',encoding='utf-8') as f:
        for name in ('anchor_exact','anchor_ambiguous','anchor_yearly'):
            f.write(f'{name}\t{items[name]}\n')
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
    # V4-07 added the caller's viewerRole to the household body without a version bump:
    # sources before v4.0.0 must not expose it, and v4.0.0 already ships the admin role,
    # so it must report exactly the value the upgrade keeps. Only the missing field is
    # normalized; an unexpected source value is a failure, not something to overwrite.
    if 'household' in expected:
        source=os.environ['UPGRADE_SMOKE_SOURCE']; household=expected['household']['body']
        if source=='v4.0.0':
            assert household.get('viewerRole')=='admin',('v4.0.0 household viewerRole',household.get('viewerRole'))
        else:
            assert 'viewerRole' not in household,('source release before v4.0.0 already exposed viewerRole',source,household.get('viewerRole'))
        household['viewerRole']='admin'
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
    member,member_session=login('member_upgrade',s['memberPassword']); assert member
    owner,owner_session=login('owner_upgrade',s['ownerPassword'])
    # Migration 022 converts only the provable setup account into an instance
    # administrator; an invited household member never becomes one.
    assert owner_session.get('instanceAdmin') is True,('setup account instanceAdmin after upgrade',owner_session)
    assert member_session.get('instanceAdmin') is False,('invited member instanceAdmin after upgrade',member_session)
    _,carried=ok(200,'GET','/api/v1/session',cookie=s['v1Session'])
    assert carried.get('instanceAdmin') is True,('source-release session instanceAdmin after upgrade',carried)
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
    # V5 anchor recovery over source-release data. The exact item kept a continuous
    # receipt chain, so migration 021 restored anchor 31 (29 for the Feb 29 yearly item);
    # the edited item is ambiguous and keeps the conservative clamped day 28.
    def done(name,completed_on,key):
        iid=s['items'][name]; hs,_=ok(200,'GET',f'{base}/items/{iid}',cookie=owner)
        _,receipt=ok(201,'POST',f'{base}/items/{iid}/completions',{'completedOn':completed_on},owner,{'If-Match':hs['ETag'],'Idempotency-Key':key})
        return hs,receipt
    before_hs,exact_receipt=done('anchor_exact','2025-02-28','upgrade-anchor-exact-next')
    assert exact_receipt['nextAttentionOn']=='2025-03-31',exact_receipt
    anchored_hs,_=ok(200,'GET',f'{base}/items/{s["items"]["anchor_exact"]}',cookie=owner)
    assert anchored_hs['ETag']!=before_hs['ETag'],anchored_hs
    _,exact_undone=ok(200,'PATCH',f'{base}/items/{s["items"]["anchor_exact"]}/completions/{exact_receipt["id"]}',{'undone':True},owner,{'If-Match':anchored_hs['ETag']})
    assert exact_undone['undoneAt'] is not None,exact_undone
    restored=ok(200,'GET',f'{base}/items/{s["items"]["anchor_exact"]}',cookie=owner)[1]
    assert restored['attentionOn']=='2025-02-28',restored
    _,exact_again=done('anchor_exact','2025-02-28','upgrade-anchor-exact-again')
    assert exact_again['nextAttentionOn']=='2025-03-31',exact_again
    _,ambiguous_receipt=done('anchor_ambiguous','2025-02-28','upgrade-anchor-ambiguous-next')
    assert ambiguous_receipt['nextAttentionOn']=='2025-03-28',ambiguous_receipt
    # Yearly: the migrated cycle is completed on its own date, then the next cycles are
    # consumed early with the same past date. A future completedOn is rejected, so early
    # completions are the only way to reach the 2028 leap day; the leap day proves the
    # recovered anchor 29 (the conservative day 28 would return 2028-02-28).
    for index,(completed_on,next_on) in enumerate((('2025-02-28','2026-02-28'),('2026-02-28','2027-02-28'),('2026-02-28','2028-02-29'))):
        _,yearly_receipt=done('anchor_yearly',completed_on,f'upgrade-anchor-yearly-next-{index}')
        assert yearly_receipt['nextAttentionOn']==next_on,(completed_on,next_on,yearly_receipt)
    print('Anchor recovery passed: exact monthly -> 2025-03-31, ambiguous monthly -> 2025-03-28, yearly leap day -> 2028-02-29.')
    _,oldmembers=ok(200,'GET',base+'/members',cookie=owner)
    ok(201,'POST','/api/v1/auth/invitations/accept',{'token':s['pendingToken'],'login':'late_upgrade','password':os.urandom(18).hex()})
    _,newmembers=ok(200,'GET',base+'/members',cookie=owner); assert len(newmembers['items'])==len(oldmembers['items'])+1
    print('Summaries, search, attention pagination, source-release session, password login, completion replay/undo, anchor recovery, create/search, and pending invitation passed.')
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
snapshot_recurrence "${compose_source[@]}" >"$temp_dir/recurrence-before.txt"
declare -A recurrence_item
while IFS=$'\t' read -r recurrence_name recurrence_id; do recurrence_item[$recurrence_name]=$recurrence_id; done <"$temp_dir/recurrence.tsv"

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
# Migration 021 adds the anchor next to the stored date; it must never rewrite an
# attention date, a completion receipt or an item version. Compare the whole schedule
# state, then assert the recovered anchor per fixture item.
snapshot_recurrence "${compose_build[@]}" >"$temp_dir/recurrence-after.txt"
if ! cmp -s "$temp_dir/recurrence-before.txt" "$temp_dir/recurrence-after.txt"; then
  diff -u "$temp_dir/recurrence-before.txt" "$temp_dir/recurrence-after.txt" >&2 || true
  printf 'Migration 021 rewrote item attention dates, versions or completion receipts\n' >&2
  exit 1
fi
for entry in anchor_exact:31:2025-02-28:2 anchor_ambiguous:28:2025-02-28:3 anchor_yearly:29:2025-02-28:2; do
  IFS=: read -r recurrence_name expected_anchor expected_attention expected_version <<<"$entry"
  actual_anchor=$(item_column attention_anchor_day "$recurrence_name")
  actual_attention=$(item_column attention_on "$recurrence_name")
  actual_version=$(item_column version "$recurrence_name")
  [[ "$actual_anchor" == "$expected_anchor" ]] || { printf 'Item %s attention_anchor_day is %s, expected %s after migration 021\n' "$recurrence_name" "$actual_anchor" "$expected_anchor" >&2; exit 1; }
  [[ "$actual_attention" == "$expected_attention" ]] || { printf 'Item %s attention_on is %s, expected %s (migration must not rewrite dates)\n' "$recurrence_name" "$actual_attention" "$expected_attention" >&2; exit 1; }
  [[ "$actual_version" == "$expected_version" ]] || { printf 'Item %s version is %s, expected %s (migration must not bump versions)\n' "$recurrence_name" "$actual_version" "$expected_version" >&2; exit 1; }
done
receipt_anchors=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM item_completions WHERE cycle_anchor_day IS NOT NULL OR next_anchor_day IS NOT NULL")
[[ "$receipt_anchors" == 0 ]] || { printf 'Migration 021 invented anchors on %s legacy receipts\n' "$receipt_anchors" >&2; exit 1; }
anchor_invariant=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM items WHERE (attention_on IS NULL) <> (attention_anchor_day IS NULL) OR attention_anchor_day < extract(day FROM attention_on)")
[[ "$anchor_invariant" == 0 ]] || { printf 'Migration 021 left %s items with an inconsistent anchor\n' "$anchor_invariant" >&2; exit 1; }
printf 'Migration 021 recovered anchor 31 and 29 from continuous receipt chains, kept 28 for the ambiguous item, and rewrote no attention date, receipt or version.\n'
instance_admins=$("${compose_build[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT string_agg(u.login || ':' || a.grant_source, ',' ORDER BY u.login) FROM instance_admins a JOIN user_accounts u ON u.id = a.user_id")
[[ "$instance_admins" == "owner_upgrade:v4_bootstrap_migration" ]] || { printf 'Migration 022 instance administrators unexpected: %s\n' "$instance_admins" >&2; exit 1; }
printf 'Migration 022 granted instance administrator to the setup account only.\n'
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
unexpected = set(after) - set(before) - {'household_role_changes', 'instance_admins', 'member_person_migration_unlinks', 'oidc_pending_logins', 'password_reset_tokens'}
assert not unexpected, ('unexpected new application tables during upgrade', sorted(unexpected))
# Migration tables must exist afterwards. A source release that already shipped the
# migration keeps its rows (the generic preservation check above); a source that
# predates it must get an empty table. member_person_migration_unlinks may hold
# deliberate non-canonical unlink rows, so only its existence is required here and its
# count is asserted against the measured expectation below.
for name, migration in (('household_role_changes', '017'), ('oidc_pending_logins', '019'), ('password_reset_tokens', '020')):
    assert name in after, (f'migration {migration} table missing after upgrade', sorted(after))
    if name not in before:
        assert after[name] == '0', (f'migration {migration} must create {name} empty', name, after[name])
assert 'member_person_migration_unlinks' in after, ('migration 018 table missing after upgrade', sorted(after))
# Every published source created its single household, the setup account and its
# local credential in one setup transaction, so migration 022 must grant exactly one
# instance administrator (identity asserted by SQL and the session API below).
assert 'instance_admins' not in before, ('source release already had instance_admins', sorted(before))
assert after.get('instance_admins') == '1', ('migration 022 bootstrap instance administrator count', after.get('instance_admins'))
assert os.environ['ACCOUNTS_WITH_EMAIL'] == '0', ('migration 019 must never infer an email for pre-existing accounts', os.environ['ACCOUNTS_WITH_EMAIL'])
assert int(os.environ['UNLINKS_AFTER']) == expected_unlinks, ('member person unlink log count', expected_unlinks, os.environ['UNLINKS_AFTER'])
assert os.environ['MEMBERSHIPS_WITHOUT_SINGLE'] == '0', ('membership without exactly one linked person', os.environ['MEMBERSHIPS_WITHOUT_SINGLE'])
before_ids = set(open(sys.argv[3], encoding='utf-8').read().split())
after_ids = set(open(sys.argv[4], encoding='utf-8').read().split())
missing = sorted(before_ids - after_ids)
assert not missing, ('pre-existing subject ids disappeared during upgrade', missing[:10])
print(f'{len(before)} pre-existing table row counts preserved; subjects +{expected_backfill} canonical member persons; {expected_unlinks} duplicate links logged; migration tables present and newly created ones empty.')
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
