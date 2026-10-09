#!/usr/bin/env bash
# Full household backup/restore evidence: populate installation A through the public API,
# back it up with the documented pg_dump command, restore into a brand-new installation B with
# fresh credentials, and prove B serves identical household data and keeps working.
set -Eeuo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
suffix="${GITHUB_RUN_ID:-local}-$$-${RANDOM}"
project_a="tendo-household-backup-a-${suffix}"
project_b="tendo-household-backup-b-${suffix}"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/tendo-household-backup.XXXXXX")
export POSTGRES_PASSWORD TENDO_DATABASE_PASSWORD TENDO_HOST_PORT TENDO_RESTART_POLICY TENDO_DB_TIMEOUT TENDO_SHUTDOWN_TIMEOUT TENDO_PUBLIC_URL TENDO_TRUSTED_PROXY_CIDRS TENDO_SETUP_TOKEN TENDO_OIDC_ISSUER TENDO_OIDC_CLIENT_ID TENDO_OIDC_CLIENT_SECRET TENDO_OIDC_CLIENT_SECRET_FILE TENDO_OIDC_DISPLAY_NAME
TENDO_OIDC_ISSUER=
TENDO_OIDC_CLIENT_ID=
TENDO_OIDC_CLIENT_SECRET=
TENDO_OIDC_CLIENT_SECRET_FILE=
TENDO_OIDC_DISPLAY_NAME=
TENDO_TRUSTED_PROXY_CIDRS=
TENDO_RESTART_POLICY=no
TENDO_DB_TIMEOUT=2
TENDO_SHUTDOWN_TIMEOUT=10
# Installation B reuses the same public URL, as a restore onto a rebuilt server would.
TENDO_HOST_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
TENDO_PUBLIC_URL="http://localhost:${TENDO_HOST_PORT}"
origin=$TENDO_PUBLIC_URL

compose_a=(docker compose --project-name "$project_a" -f "$root/compose.yaml")
compose_b=(docker compose --project-name "$project_b" -f "$root/compose.yaml")
cleanup() {
  local result=$?
  trap - EXIT
  local project
  for project in "$project_a" "$project_b"; do
    if ! docker compose --project-name "$project" -f "$root/compose.yaml" down --volumes --remove-orphans --rmi local; then
      printf 'Compose cleanup failed for project %s\n' "$project" >&2
      (( result != 0 )) || result=1
    fi
  done
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

# Exact row counts for every base table in schema public, plus the migration ledger.
snapshot_counts() {
  "$@" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At <<'SQL'
SELECT table_name || '=' || (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', table_schema, table_name), false, true, '')))[1]::text
FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name;
SELECT 'migration ' || version || ' dirty=' || dirty FROM tendo_schema_migrations ORDER BY version;
SQL
}

api() {
  HOUSEHOLD_SMOKE_ORIGIN="$origin" HOUSEHOLD_SMOKE_STATE="$temp_dir/state.json" python3 "$temp_dir/api.py" "$1"
}
cat >"$temp_dir/api.py" <<'PY'
import json, os, sys, urllib.error, urllib.request

origin = os.environ['HOUSEHOLD_SMOKE_ORIGIN']
state_path = os.environ['HOUSEHOLD_SMOKE_STATE']
phase = sys.argv[1]

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None
opener = urllib.request.build_opener(NoRedirect)

def req(method, path, body=None, cookie=None, headers=None):
    h = dict(headers or {})
    data = None
    if cookie:
        h['Cookie'] = f'tendo_session={cookie}'
    if method != 'GET':
        h['Origin'] = origin
    if body is not None:
        h['Content-Type'] = 'application/json'
        data = json.dumps(body, ensure_ascii=False).encode()
    r = urllib.request.Request(origin + path, data=data, headers=h, method=method)
    try:
        with opener.open(r, timeout=10) as x:
            status, hs, raw = x.status, x.headers, x.read()
    except urllib.error.HTTPError as e:
        status, hs, raw = e.code, e.headers, e.read()
    return status, hs, (json.loads(raw) if raw else None)

def ok(expected, method, path, body=None, cookie=None, headers=None):
    status, hs, b = req(method, path, body, cookie, headers)
    assert status == expected, (method, path, status, b)
    return hs, b

def login(name, password):
    hs, b = ok(201, 'POST', '/api/v1/session', {'login': name, 'password': password})
    cookie = hs['Set-Cookie'].split(';', 1)[0]
    key, _, token = cookie.partition('=')
    assert key == 'tendo_session' and len(token) == 43, cookie
    return token, b

def all_pages(path, cookie):
    out, cursor = [], None
    while True:
        sep = '&' if '?' in path else '?'
        page_path = path + (f'{sep}cursor={cursor}' if cursor else '')
        hs, b = ok(200, 'GET', page_path, cookie=cookie)
        out.append({'path': page_path, 'etag': hs.get('ETag'), 'body': b})
        cursor = b['nextCursor']
        if not cursor:
            return out

def capture(state):
    cookie, hid = state['ownerSession'], state['householdId']
    base = f'/api/v1/households/{hid}'
    hs, b = ok(200, 'GET', base, cookie=cookie)
    snap = {'household': {'etag': hs.get('ETag'), 'body': b}, 'members': all_pages(f'{base}/members?limit=1', cookie)}
    for archived in ('false', 'true'):
        snap[f'subjects archived={archived}'] = all_pages(f'{base}/subjects?archived={archived}&limit=2', cookie)
    item_ids = set()
    for done in ('false', 'true'):
        for archived in ('false', 'true'):
            pages = all_pages(f'{base}/items?done={done}&archived={archived}&limit=2', cookie)
            snap[f'items done={done} archived={archived}'] = pages
            item_ids.update(i['id'] for p in pages for i in p['body']['items'])
    assert item_ids == set(state['items'].values()), ('listed items differ from created items', item_ids, state['items'])
    for name, iid in sorted(state['items'].items()):
        hs, b = ok(200, 'GET', f'{base}/items/{iid}', cookie=cookie)
        snap[f'item {name}'] = {'etag': hs.get('ETag'), 'body': b}
        snap[f'completions {name}'] = all_pages(f'{base}/items/{iid}/completions?limit=1', cookie)
    return snap

def populate():
    owner_password = os.urandom(18).hex()
    member_password = os.urandom(18).hex()
    ok(201, 'PUT', '/api/v1/auth/setup', {'login': 'owner_backup', 'password': owner_password, 'householdName': 'Novákovi 家族', 'timezone': 'Europe/Prague'}, headers={'X-Tendo-Setup-Token': os.environ['TENDO_SETUP_TOKEN']})
    owner, session = login('owner_backup', owner_password)
    hid = session['defaultHouseholdId']
    base = f'/api/v1/households/{hid}'
    _, person = ok(201, 'POST', f'{base}/subjects', {'name': 'Žofie', 'type': 'person'}, owner)
    _, car = ok(201, 'POST', f'{base}/subjects', {'name': 'Octavia', 'type': 'vehicle'}, owner)
    _, old = ok(201, 'POST', f'{base}/subjects', {'name': 'Old shed', 'type': 'home'}, owner)
    ok(200, 'PATCH', f'{base}/subjects/{old["id"]}', {'archived': True}, owner, {'If-Match': '"1"'})
    _, invitation = ok(201, 'POST', f'{base}/invitations', {}, owner, {'Idempotency-Key': 'backup-member'})
    ok(201, 'POST', '/api/v1/auth/invitations/accept', {'token': invitation['token'], 'login': 'member_backup', 'password': member_password})
    login('member_backup', member_password)
    _, members = ok(200, 'GET', f'{base}/members', cookie=owner)
    member_id = next(m['userId'] for m in members['items'] if m['login'] == 'member_backup')

    items = {}
    def create(name, body):
        _, b = ok(201, 'POST', f'{base}/items', body, owner)
        items[name] = b['id']
        return b
    def complete(name, version, key, completed_on):
        _, b = ok(201, 'POST', f'{base}/items/{items[name]}/completions', {'completedOn': completed_on}, owner, {'If-Match': f'"{version}"', 'Idempotency-Key': key})
        return b
    # Past dates stay needs_attention and future dates stay upcoming for the whole run.
    create('oneoff', {'title': 'Book dental check', 'subjectId': person['id'], 'notes': 'Ask about the\nwinter slot.', 'attentionOn': '2025-03-01', 'responsibleUserId': member_id})
    fixed = create('fixed_historical', {'title': 'Replace water filter', 'subjectId': person['id'], 'historicalCompletedOn': '2024-01-31', 'recurrence': {'intervalValue': 1, 'intervalUnit': 'month', 'mode': 'fixed'}})
    assert fixed['attentionOn'] == '2024-02-29' and fixed['attention'] == 'needs_attention', fixed
    create('fluid', {'title': 'Water the plants', 'subjectId': person['id'], 'attentionOn': '2025-01-01', 'recurrence': {'intervalValue': 1, 'intervalUnit': 'week', 'mode': 'after_completion'}})
    fluid_receipt = complete('fluid', 1, 'restore-replay-key', '2025-01-05')
    assert fluid_receipt['nextAttentionOn'] == '2025-01-12', fluid_receipt
    create('yearly', {'title': 'Car inspection', 'subjectId': car['id'], 'attentionOn': '2020-03-01', 'recurrence': {'intervalValue': 1, 'intervalUnit': 'year', 'mode': 'fixed'}})
    complete('yearly', 1, 'yearly-1', '2025-02-01')
    second = complete('yearly', 2, 'yearly-2', '2025-02-02')
    ok(200, 'PATCH', f'{base}/items/{items["yearly"]}/completions/{second["id"]}', {'undone': True}, owner, {'If-Match': '"3"'})
    create('done_oneoff', {'title': 'Buy winter tyres', 'subjectId': car['id']})
    complete('done_oneoff', 1, 'done-1', '2025-02-03')
    create('archived', {'title': 'Paint the fence', 'subjectId': person['id'], 'attentionOn': '2031-05-01'})
    ok(200, 'PATCH', f'{base}/items/{items["archived"]}', {'archived': True}, owner, {'If-Match': '"1"'})
    _, pending = ok(201, 'POST', f'{base}/invitations', {}, owner, {'Idempotency-Key': 'backup-pending'})
    state = {'ownerSession': owner, 'ownerPassword': owner_password, 'memberPassword': member_password, 'householdId': hid,
             'personId': person['id'], 'items': items, 'fluidReceipt': fluid_receipt, 'pendingToken': pending['token']}
    state['before'] = capture(state)
    attention = {i['attention'] for p in state['before']['items done=false archived=false'] for i in p['body']['items']}
    assert 'needs_attention' in attention, attention
    with open(state_path, 'w', encoding='utf-8') as f:
        json.dump(state, f, ensure_ascii=False)

def verify():
    with open(state_path, encoding='utf-8') as f:
        state = json.load(f)
    after = capture(state)
    assert after.keys() == state['before'].keys(), (sorted(after), sorted(state['before']))
    for key in state['before']:
        assert after[key] == state['before'][key], ('restored response differs', key, state['before'][key], after[key])
    print(f'{len(after)} captured responses identical after restore (bodies and ETags)')

def exercise():
    with open(state_path, encoding='utf-8') as f:
        state = json.load(f)
    login('member_backup', state['memberPassword'])
    owner, _ = login('owner_backup', state['ownerPassword'])
    base = f'/api/v1/households/{state["householdId"]}'
    fluid = state['items']['fluid']
    before = state['before']['item fluid']
    # The completion idempotency record survives restore: the same key replays the stored receipt.
    _, replay = ok(201, 'POST', f'{base}/items/{fluid}/completions', {'completedOn': '2025-01-05'}, owner, {'If-Match': before['etag'], 'Idempotency-Key': 'restore-replay-key'})
    assert replay == state['fluidReceipt'], (replay, state['fluidReceipt'])
    hs, item = ok(200, 'GET', f'{base}/items/{fluid}', cookie=owner)
    assert hs['ETag'] == before['etag'] and item == before['body'], ('replay changed the item', item)
    hs, undone = ok(200, 'PATCH', f'{base}/items/{fluid}/completions/{replay["id"]}', {'undone': True}, owner, {'If-Match': hs['ETag']})
    assert undone['undoneAt'] is not None, undone
    _, item = ok(200, 'GET', f'{base}/items/{fluid}', cookie=owner)
    assert item['attentionOn'] == '2025-01-01' and item['lastCompletedOn'] is None, item
    _, subject = ok(201, 'POST', f'{base}/subjects', {'name': 'Garden', 'type': 'home'}, owner)
    _, created = ok(201, 'POST', f'{base}/items', {'title': 'Trim the hedge', 'subjectId': subject['id']}, owner)
    assert created['attention'] == 'needs_attention', created
    _, members = ok(200, 'GET', f'{base}/members', cookie=owner)
    ok(201, 'POST', '/api/v1/auth/invitations/accept', {'token': state['pendingToken'], 'login': 'late_member', 'password': os.urandom(18).hex()})
    _, grown = ok(200, 'GET', f'{base}/members', cookie=owner)
    assert len(grown['items']) == len(members['items']) + 1 and 'late_member' in {m['login'] for m in grown['items']}, grown
    print('restored installation accepts sign-in, completion replay, undo, new records, and the pending invitation')

{'populate': populate, 'verify': verify, 'exercise': exercise}[phase]()
PY

# Installation A: populate a household through the public API, then back it up.
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=$(openssl rand -base64 32)
"${compose_a[@]}" up -d --build --wait --wait-timeout 120
wait_ready
api populate
"${compose_a[@]}" stop app
snapshot_counts "${compose_a[@]}" >"$temp_dir/counts-before.txt"
grep -qx 'items=6' "$temp_dir/counts-before.txt"
# Documented backup command (docs/admin/backup-restore.md).
"${compose_a[@]}" exec -T postgres pg_dump -U postgres -d tendo --format=custom >"$temp_dir/tendo-backup.dump"
test -s "$temp_dir/tendo-backup.dump"
"${compose_a[@]}" down --volumes --remove-orphans

# Installation B: new volume and new credentials; the dump carries no role passwords.
POSTGRES_PASSWORD=$(openssl rand -hex 32)
TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)
TENDO_SETUP_TOKEN=
# Documented restore commands (docs/admin/backup-restore.md).
"${compose_b[@]}" up -d --wait --wait-timeout 90 postgres
tables=$("${compose_b[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d tendo -At -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
if [[ "$tables" != 0 ]]; then printf 'Restore target is not empty: %s tables\n' "$tables" >&2; exit 1; fi
"${compose_b[@]}" exec -T postgres pg_restore --exit-on-error --single-transaction -U postgres -d tendo <"$temp_dir/tendo-backup.dump"
"${compose_b[@]}" up -d --build --wait --wait-timeout 120
wait_ready
snapshot_counts "${compose_b[@]}" >"$temp_dir/counts-after.txt"
if ! diff -u "$temp_dir/counts-before.txt" "$temp_dir/counts-after.txt"; then
  printf 'Restored row counts differ from the backed-up installation\n' >&2
  exit 1
fi
api verify
api exercise
"${compose_b[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U tendo -d tendo <<'SQL'
DO $$ BEGIN
  IF (SELECT count(*) FROM items) <> 7 THEN RAISE EXCEPTION 'runtime role cannot read restored items'; END IF;
  BEGIN
    DELETE FROM items;
    RAISE EXCEPTION 'restored runtime role unexpectedly deleted items';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    TRUNCATE item_completions;
    RAISE EXCEPTION 'restored runtime role unexpectedly truncated completions';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
END $$;
SQL
printf 'Full household backup/restore smoke passed.\n'
