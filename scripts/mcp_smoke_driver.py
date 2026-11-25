#!/usr/bin/env python3
"""Drive the live MCP OAuth smoke against a Compose app (V5-12 A4c, ADR 0023).

Usage: mcp_smoke_driver.py <phase> <state.json>, phase is one of enabled,
installation, disabled, reenabled. Environment: MCP_SMOKE_ORIGIN, MCP_SMOKE_SETUP_TOKEN,
MCP_SMOKE_PSQL (a JSON array argv for psql), MCP_SMOKE_FIXTURES,
MCP_SMOKE_SECRETS. Failures raise SmokeFailure explicitly (not assert, which
python -O strips), and the report is redacted of every collected secret.

The script is the MCP client and the browser at once: it registers a public
client, runs PKCE authorization through the consent API with a signed-in
session, exchanges codes and calls /mcp. Every MCP response it records is a
fixture for api/check-health.mjs. Each grant has one role, because code replay
and refresh replay revoke the whole grant (ADR 0023):
  A  wrong verifier, exchange, code replay (grant dies)
  B  refresh rotation, then old refresh replay (grant dies)
  C  /mcp protocol checks, then RFC 7009 revocation by the client
  D  disconnect through Connected apps
  E  stays live across the disable and re-enable phases
  W  tendo:write only: no tool scope, so /mcp is 403 insufficient_scope
  M  a second account: cross-household isolation
"""
import base64, hashlib, json, os, secrets as rnd, subprocess, sys, traceback
import urllib.error, urllib.parse, urllib.request

ORIGIN = os.environ['MCP_SMOKE_ORIGIN']
RESOURCE = ORIGIN + '/mcp'
FOREIGN = 'http://foreign.invalid'
REDIRECT = 'http://127.0.0.1:9/callback'
PROTOCOL = '2025-11-25'
CONSENT = '/api/v1/mcp/authorization-requests/{requestHandle}'
DECISION = CONSENT + '/decision'
GRANT = '/api/v1/mcp/grants/{grantId}'
PASSWORD = 'correct horse battery staple'


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


opener = urllib.request.build_opener(NoRedirect)
fixtures = []


class SmokeFailure(Exception):
    pass


def check(condition, *detail):
    if not condition:
        raise SmokeFailure(detail)


def secret(value):
    if value:
        with open(os.environ['MCP_SMOKE_SECRETS'], 'a', encoding='utf-8') as f:
            f.write(value + '\n')


def send(method, url, template=None, json_body=None, form=None, raw=None, headers=None):
    """Send one request; return status, headers, raw payload and decoded body.
    A template records the response as a fixture."""
    hs = dict(headers or {})
    data = None
    if json_body is not None:
        hs.setdefault('Content-Type', 'application/json')
        data = json.dumps(json_body).encode()
    elif form is not None:
        hs.setdefault('Content-Type', 'application/x-www-form-urlencoded')
        data = urllib.parse.urlencode(form).encode()
    elif raw is not None:
        data = raw
    req = urllib.request.Request(ORIGIN + url, data=data, headers=hs, method=method)
    try:
        with opener.open(req, timeout=15) as r:
            status, h, payload = r.status, r.headers, r.read()
    except urllib.error.HTTPError as e:
        status, h, payload = e.code, e.headers, e.read()
    for cookie in h.get_all('Set-Cookie') or []:
        secret(cookie.split(';', 1)[0].split('=', 1)[-1])
    location = h.get('Location') or ''
    if '#request=' in location:
        secret(location.split('#request=', 1)[1])
    media = (h.get('Content-Type') or '').split(';')[0].strip()
    body = None
    if payload:
        body = payload.decode() if media == 'text/plain' else json.loads(payload)
    if isinstance(body, dict):
        # Recorded before any check, so a failure report is always redacted.
        for key in ('access_token', 'refresh_token', 'token'):
            if isinstance(body.get(key), str):
                secret(body[key])
        if isinstance(body.get('redirectTo'), str):
            for code in urllib.parse.parse_qs(urllib.parse.urlsplit(body['redirectTo']).query).get('code', []):
                secret(code)
    if template:
        fixture = {
            'path': template, 'url': url.split('?', 1)[0], 'method': method.lower(), 'status': status,
            'headers': {k.lower(): (h.get_all(k) if k.lower() == 'set-cookie' else h.get_all(k)[0]) for k in h.keys()},
        }
        if payload:
            fixture['body'] = body
        fixtures.append(fixture)
    return status, h, payload, body


def request(*args, **kwargs):
    status, h, _, body = send(*args, **kwargs)
    return status, h, body


def psql(sql):
    out = subprocess.check_output(json.loads(os.environ['MCP_SMOKE_PSQL']) + ['-c', sql], text=True)
    return out.strip().splitlines()[-1] if out.strip() else ''


def cookie_pair(headers, name):
    for value in headers.get_all('Set-Cookie') or []:
        pair = value.split(';', 1)[0]
        if pair.startswith(name + '=') and pair.split('=', 1)[1]:
            return pair
    raise SmokeFailure(('missing cookie', name, headers.get_all('Set-Cookie')))


def login(user):
    status, h, body = request('POST', '/api/v1/session', json_body={'login': user, 'password': PASSWORD},
                              headers={'Origin': ORIGIN})
    check(status == 201, ('login', user, status, body))
    return cookie_pair(h, 'tendo_session')


def pkce():
    verifier = base64.urlsafe_b64encode(rnd.token_bytes(32)).rstrip(b'=').decode()
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b'=').decode()
    secret(verifier)
    return verifier, challenge


def authorize_url(client_id, challenge, state, scope=None, resource=RESOURCE, method='S256'):
    query = {'response_type': 'code', 'client_id': client_id, 'redirect_uri': REDIRECT,
             'code_challenge': challenge, 'code_challenge_method': method, 'state': state}
    if scope is not None:
        query['scope'] = scope
    if resource is not None:
        query['resource'] = resource
    return '/oauth/authorize?' + urllib.parse.urlencode(query)


def begin(client_id, scope=None):
    """Authorize: a 303 to /connect#request=<handle> with the binding cookie."""
    verifier, challenge = pkce()
    state = rnd.token_urlsafe(12)
    status, h, body = request('GET', authorize_url(client_id, challenge, state, scope), '/oauth/authorize')
    location = h.get('Location', '')
    check(status == 303 and body is None and location.startswith('/connect#request='), ('authorize', status, location))
    handle = location.split('=', 1)[1]
    secret(handle)
    binding = cookie_pair(h, 'tendo_mcp_consent')
    return {'verifier': verifier, 'state': state, 'handle': handle, 'binding': binding}


def decide(flow, session, decision, expect=200):
    url = f"/api/v1/mcp/authorization-requests/{flow['handle']}/decision"
    status, h, body = request('POST', url, DECISION, json_body={'decision': decision},
                              headers={'Origin': ORIGIN, 'Cookie': session + '; ' + flow['binding']})
    check(status == expect, ('decision', decision, status, body))
    if status != 200:
        return body
    target = urllib.parse.urlsplit(body['redirectTo'])
    check(f'{target.scheme}://{target.netloc}{target.path}' == REDIRECT, body)
    query = urllib.parse.parse_qs(target.query, strict_parsing=True)
    check(query['state'] == [flow['state']] and query['iss'] == [ORIGIN], query)
    if decision == 'approve':
        check(set(query) == {'code', 'state', 'iss'}, query)
        secret(query['code'][0])
        return query['code'][0]
    check(set(query) == {'error', 'state', 'iss'} and query['error'] == ['access_denied'], query)
    return None


def token(form, expect=200, error=None):
    status, _, body = request('POST', '/oauth/token', '/oauth/token', form=form)
    check(status == expect, ('token', form.get('grant_type'), status, body))
    if error:
        check(body['error'] == error and set(body) == {'error', 'error_description'}, body)
    if status == 200:
        check(body['token_type'] == 'Bearer' and 0 < body['expires_in'] <= 600, body)
        secret(body['access_token'])
        secret(body['refresh_token'])
    return body


def exchange(client_id, flow, code, expect=200, error=None, verifier=None):
    return token({'grant_type': 'authorization_code', 'code': code, 'redirect_uri': REDIRECT,
                  'client_id': client_id, 'code_verifier': verifier or flow['verifier']}, expect, error)


def refresh(client_id, tokens, expect=200, error=None):
    return token({'grant_type': 'refresh_token', 'refresh_token': tokens['refresh_token'],
                  'client_id': client_id}, expect, error)


def connect(client_id, session, scope=None):
    flow = begin(client_id, scope)
    code = decide(flow, session, 'approve')
    return exchange(client_id, flow, code)


def mcp(access, message, version=PROTOCOL, origin=None, raw=None, expect=200):
    headers = {'Accept': 'application/json, text/event-stream', 'Content-Type': 'application/json'}
    if access is not None:
        headers['Authorization'] = 'Bearer ' + access
    if version:
        headers['MCP-Protocol-Version'] = version
    if origin:
        headers['Origin'] = origin
    status, h, body = request('POST', '/mcp', '/mcp', json_body=message if raw is None else None, raw=raw, headers=headers)
    check(status == expect, ('mcp', message, status, body))
    return h, body


def households(access):
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call',
                           'params': {'name': 'list_households', 'arguments': {}}})
    result = body['result']
    check(result.get('isError') is not True, body)
    return {(row['id'], row['name'], row['role']) for row in result['structuredContent']['households']}


READ_TOOLS = ['list_households', 'list_items', 'get_item', 'list_completions', 'list_subjects', 'get_subject']


def tool(access, name, arguments):
    """One tools/call; returns (structuredContent, None) or (None, error text)."""
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 4, 'method': 'tools/call',
                           'params': {'name': name, 'arguments': arguments}})
    result = body['result']
    if result.get('isError'):
        return None, result['content'][0]['text']
    return result['structuredContent'], None


def read_tools(access, owner, home):
    """The read tools against records created through the browser API."""
    status, _, subject = request('POST', f'/api/v1/households/{home}/subjects', json_body={'type': 'vehicle', 'name': 'Smoke car'},
                                 headers={'Cookie': owner, 'Origin': ORIGIN, 'Idempotency-Key': 'mcp-smoke-subject'})
    check(status == 201, ('create subject', status, subject))
    status, _, item = request('POST', f'/api/v1/households/{home}/items', json_body={'subjectId': subject['id'], 'title': 'Smoke oil change'},
                              headers={'Cookie': owner, 'Origin': ORIGIN, 'Idempotency-Key': 'mcp-smoke-item'})
    check(status == 201, ('create item', status, item))
    found, err = tool(access, 'list_items', {'householdId': home, 'q': 'oil'})
    check(err is None and [row['id'] for row in found['items']] == [item['id']] and found['nextCursor'] == '', ('list_items', found, err))
    found, err = tool(access, 'get_item', {'householdId': home, 'itemId': item['id']})
    check(err is None and found['title'] == 'Smoke oil change' and found['subjectId'] == subject['id'], ('get_item', found, err))
    found, err = tool(access, 'list_completions', {'householdId': home, 'itemId': item['id']})
    check(err is None and found['completions'] == [], ('list_completions', found, err))
    found, err = tool(access, 'list_subjects', {'householdId': home})
    check(err is None and subject['id'] in {row['id'] for row in found['subjects']}, ('list_subjects', found, err))
    found, err = tool(access, 'get_subject', {'householdId': home, 'subjectId': subject['id']})
    check(err is None and found['name'] == 'Smoke car', ('get_subject', found, err))
    missing = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff'
    _, unknown_house = tool(access, 'list_items', {'householdId': missing})
    _, unknown_item = tool(access, 'get_item', {'householdId': home, 'itemId': missing})
    check(unknown_house == unknown_item == 'the household or record was not found', (unknown_house, unknown_item))


def mcp_refused(access):
    h, body = mcp(access, {'jsonrpc': '2.0', 'id': 9, 'method': 'tools/list'}, expect=401)
    check(body['error'] == 'invalid_token' and 'error="invalid_token"' in h['WWW-Authenticate'], (h, body))


def grant_of(access):
    digest = hashlib.sha256(access.encode()).hexdigest()
    return psql(f"SELECT grant_id FROM mcp_access_tokens WHERE token_hash=decode('{digest}','hex')")


def unknown_path_404():
    status, h, payload, _ = send('GET', '/api/v1/no-such-route')
    check(status == 404, status)
    return h, payload


def setup_owner():
    status, _, body = request('PUT', '/api/v1/auth/setup', json_body={
        'login': 'mcp_owner', 'password': PASSWORD, 'householdName': 'MCP Smoke', 'timezone': 'UTC'},
        headers={'Origin': ORIGIN, 'X-Tendo-Setup-Token': os.environ['MCP_SMOKE_SETUP_TOKEN']})
    check(status == 201, ('setup', status, body))
    owner = login('mcp_owner')
    _, _, me = request('GET', '/api/v1/session', headers={'Cookie': owner})
    return owner, me['defaultHouseholdId']


def discover():
    status, _, prm = request('GET', '/.well-known/oauth-protected-resource/mcp', '/.well-known/oauth-protected-resource/mcp')
    check(status == 200 and prm['resource'] == RESOURCE and prm['authorization_servers'] == [ORIGIN], prm)
    status, _, asm = request('GET', '/.well-known/oauth-authorization-server', '/.well-known/oauth-authorization-server')
    check(status == 200 and asm['issuer'] == ORIGIN, asm)
    for key, path in [('authorization_endpoint', '/oauth/authorize'), ('token_endpoint', '/oauth/token'),
                      ('registration_endpoint', '/oauth/register'), ('revocation_endpoint', '/oauth/revoke')]:
        check(asm[key] == ORIGIN + path, (key, asm))
    check(asm['code_challenge_methods_supported'] == ['S256'], asm)


def register():
    """One client for the whole smoke: registration allows 10 per minute per IP."""
    status, _, refused = request('POST', '/oauth/register', '/oauth/register',
                                 json_body={'redirect_uris': ['javascript:alert(1)'], 'client_name': 'Refused'})
    check(status == 400 and refused['error'] == 'invalid_redirect_uri', refused)
    status, _, client = request('POST', '/oauth/register', '/oauth/register',
                                json_body={'redirect_uris': [REDIRECT], 'client_name': 'MCP Smoke Client'})
    check(status == 201 and client['token_endpoint_auth_method'] == 'none', client)
    check(client['scope'] == 'tendo:read tendo:write' and client['redirect_uris'] == [REDIRECT], client)
    return client['client_id']


def authorize_refusals(cid):
    """Authorize refusals redirect to /connect#error= and set no cookie."""
    _, challenge = pkce()
    for label, url, code in [
            ('plain PKCE', authorize_url(cid, challenge, 's', method='plain'), 'invalid_request'),
            ('foreign resource', authorize_url(cid, challenge, 's', resource='http://evil.invalid/mcp'), 'invalid_target')]:
        status, h, body = request('GET', url, '/oauth/authorize')
        check(status == 303 and h['Location'] == '/connect#error=' + code, (label, status, h['Location']))
        check(not h.get_all('Set-Cookie'), (label, h.get_all('Set-Cookie')))


def grant_a(cid, owner):
    """Consent reads, decision refusals, verifier mismatch and code reuse."""
    flow = begin(cid)
    consent_url = f"/api/v1/mcp/authorization-requests/{flow['handle']}"
    status, _, body = request('GET', consent_url, CONSENT, headers={'Cookie': flow['binding']})
    check(status == 401 and body['code'] == 'unauthenticated', ('consent without session', status, body))
    status, _, body = request('GET', consent_url, CONSENT, headers={'Cookie': owner})
    check(status == 404 and body['code'] == 'authorization_request_not_found', ('consent without binding', status, body))
    status, _, view = request('GET', consent_url, CONSENT, headers={'Cookie': owner + '; ' + flow['binding']})
    check(status == 200 and view['clientName'] == 'MCP Smoke Client' and view['clientNameVerified'] is False, view)
    check(view['redirectUri'] == REDIRECT and sorted(view['scopes']) == ['tendo:read', 'tendo:write'], view)
    status, _, body = request('POST', consent_url + '/decision', DECISION, json_body={'decision': 'approve'},
                              headers={'Origin': FOREIGN, 'Cookie': owner + '; ' + flow['binding']})
    check(status == 403, ('decision foreign Origin', status, body))
    status, _, body = request('POST', consent_url + '/decision', DECISION, json_body={'decision': 'maybe'},
                              headers={'Origin': ORIGIN, 'Cookie': owner + '; ' + flow['binding']})
    check(status == 400 and body['code'] == 'invalid_request', ('decision value', status, body))
    code = decide(flow, owner, 'approve')
    replay = decide(flow, owner, 'approve', expect=404)
    check(replay['code'] == 'authorization_request_not_found', replay)
    token({'grant_type': 'authorization_code', 'code': code, 'redirect_uri': REDIRECT, 'client_id': cid,
           'code_verifier': flow['verifier'], 'resource': 'http://evil.invalid/mcp'}, 400, 'invalid_target')
    wrong, _ = pkce()
    exchange(cid, flow, code, 400, 'invalid_grant', verifier=wrong)
    tokens = exchange(cid, flow, code)
    grant_id = grant_of(tokens['access_token'])
    exchange(cid, flow, code, 400, 'invalid_grant')
    mcp_refused(tokens['access_token'])
    refresh(cid, tokens, 400, 'invalid_grant')
    return grant_id


def denial(cid, owner):
    """Denial issues no code and creates no grant."""
    before = psql('SELECT count(*) FROM mcp_grants')
    check(decide(begin(cid), owner, 'deny') is None, 'denial issued a code')
    check(psql('SELECT count(*) FROM mcp_grants') == before, 'denial created a grant')


def grant_b(cid, owner, home):
    """Refresh rotation, then replay of the consumed refresh token."""
    tokens = connect(cid, owner)
    grant_id = grant_of(tokens['access_token'])
    rotated = refresh(cid, tokens)
    check(rotated['refresh_token'] != tokens['refresh_token'], 'refresh token not rotated')
    check(households(rotated['access_token']) == {(home, 'MCP Smoke', 'admin')}, 'rotated households')
    refresh(cid, tokens, 400, 'invalid_grant')
    refresh(cid, rotated, 400, 'invalid_grant')
    mcp_refused(rotated['access_token'])
    return grant_id


def grant_c(cid, owner, home):
    """The /mcp protocol surface, then RFC 7009 revocation by the client."""
    tokens = connect(cid, owner)
    access = tokens['access_token']
    grant_id = grant_of(access)
    h, body = mcp(None, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, expect=401)
    challenge = h['WWW-Authenticate']
    check('error=' not in challenge and f'resource_metadata="{ORIGIN}/.well-known/oauth-protected-resource/mcp"' in challenge, challenge)
    mcp_refused('A' * 43)
    mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, origin=FOREIGN, expect=403)
    _, body = mcp(access, None, raw=b'[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]', expect=400)
    check(body['error']['code'] == -32600, body)
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, version=None, expect=400)
    check(body['error']['code'] == -32600, body)
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {
        'protocolVersion': PROTOCOL, 'capabilities': {}, 'clientInfo': {'name': 'mcp-smoke', 'version': '1'}}}, version=None)
    check(body['result']['protocolVersion'] == PROTOCOL, body)
    mcp(access, {'jsonrpc': '2.0', 'method': 'notifications/initialized'}, expect=202)
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'})
    check(sorted(tool['name'] for tool in body['result']['tools']) == sorted(READ_TOOLS), body)
    check(households(access) == {(home, 'MCP Smoke', 'admin')}, 'grant C households')
    read_tools(access, owner, home)
    status, _, body = request('POST', '/oauth/revoke', '/oauth/revoke', form={'client_id': cid})
    check(status == 400 and body['error'] == 'invalid_request', ('revoke without token', status, body))
    status, _, body = request('POST', '/oauth/revoke', '/oauth/revoke', form={'token': tokens['refresh_token'], 'client_id': cid})
    check(status == 200 and body is None, ('revoke', status, body))
    mcp_refused(access)
    refresh(cid, tokens, 400, 'invalid_grant')
    return grant_id


def grant_w(cid, owner):
    """A tendo:write-only grant holds no scope any tool requires."""
    tokens = connect(cid, owner, scope='tendo:write')
    check(tokens['scope'] == 'tendo:write', tokens['scope'])
    h, body = mcp(tokens['access_token'], {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, expect=403)
    check(body['error'] == 'insufficient_scope' and 'error="insufficient_scope"' in h['WWW-Authenticate'], (h, body))


def grant_d(cid, owner):
    """Disconnect through Connected apps reaches the next request."""
    tokens = connect(cid, owner)
    grant_id = grant_of(tokens['access_token'])
    status, _, listed = request('GET', '/api/v1/mcp/grants', headers={'Cookie': owner})
    check(status == 200 and grant_id in {item['id'] for item in listed['items']}, listed)
    status, _, body = request('DELETE', '/api/v1/mcp/grants/' + grant_id, headers={'Origin': ORIGIN, 'Cookie': owner})
    check(status == 204, ('disconnect', status, body))
    mcp_refused(tokens['access_token'])
    refresh(cid, tokens, 400, 'invalid_grant')
    return grant_id


def grant_m(cid, owner, home):
    """A second account in a household the owner does not belong to."""
    status, _, invite = request('POST', f'/api/v1/households/{home}/invitations', json_body={},
                                headers={'Origin': ORIGIN, 'Cookie': owner, 'Idempotency-Key': 'mcp-smoke-member'})
    check(status == 201, ('invitation', status, invite))
    status, _, body = request('POST', '/api/v1/auth/invitations/accept', json_body={
        'token': invite['token'], 'login': 'mcp_member', 'password': PASSWORD}, headers={'Origin': ORIGIN})
    check(status == 201, ('accept', status, body))
    member = login('mcp_member')
    _, _, member_me = request('GET', '/api/v1/session', headers={'Cookie': member})
    status, _, other = request('POST', '/api/v1/admin/households', json_body={
        'name': 'Member Only', 'timezone': 'UTC', 'adminUserId': member_me['userId']},
        headers={'Origin': ORIGIN, 'Cookie': owner, 'Idempotency-Key': 'mcp-smoke-other'})
    check(status == 201, ('member household', status, other))
    tokens = connect(cid, member)
    check(households(tokens['access_token']) == {(home, 'MCP Smoke', 'member'), (other['id'], 'Member Only', 'admin')},
          'member households')
    owner_check = connect(cid, owner)
    check(households(owner_check['access_token']) == {(home, 'MCP Smoke', 'admin')}, 'owner sees a foreign household')


def phase_enabled(state):
    # Fixed order: replays revoke a whole grant, so each grant has one role.
    # /oauth/authorize allows 20 requests per minute per IP; this phase makes 11.
    owner, home = setup_owner()
    discover()
    cid = register()
    authorize_refusals(cid)
    revoked = {grant_a(cid, owner): 'code_reuse'}
    denial(cid, owner)
    revoked[grant_b(cid, owner, home)] = 'replay'
    revoked[grant_c(cid, owner, home)] = 'client'
    grant_w(cid, owner)
    revoked[grant_d(cid, owner)] = 'user'
    grant_m(cid, owner, home)
    tokens = connect(cid, owner)  # grant E survives the switch-off
    state.update(client_id=cid, owner=owner, home=home, grant_e=tokens, grant_e_id=grant_of(tokens['access_token']))
    events = psql("SELECT string_agg(grant_id || ':' || reason, ',' ORDER BY grant_id) FROM mcp_grant_events WHERE event='revoked'")
    rows = sorted(tuple(event.split(':')) for event in events.split(',') if event)
    check(rows == sorted(revoked.items()), ('revocation reasons per grant', rows, revoked))


def phase_disabled(state):
    unknown_headers, unknown = unknown_path_404()
    flow_handle = 'H' * 43
    probes = [
        ('POST', '/oauth/register', '/oauth/register', {'json_body': {'redirect_uris': [REDIRECT]}}),
        ('GET', '/.well-known/oauth-protected-resource/mcp', '/.well-known/oauth-protected-resource/mcp', {}),
        ('GET', '/.well-known/oauth-authorization-server', '/.well-known/oauth-authorization-server', {}),
        ('GET', authorize_url(state['client_id'], 'C' * 43, 's'), '/oauth/authorize', {}),
        ('GET', f'/api/v1/mcp/authorization-requests/{flow_handle}', CONSENT, {'headers': {'Cookie': state['owner']}}),
        # Asserted but not recorded: the decision 404 schema documents the
        # enabled response, which clears the consent cookie. While disabled the
        # route answers the unknown-path 404 with no cookie. The enabled 404s
        # are recorded in phase_enabled.
        ('POST', f'/api/v1/mcp/authorization-requests/{flow_handle}/decision', None,
         {'json_body': {'decision': 'approve'}, 'headers': {'Origin': ORIGIN, 'Cookie': state['owner']}}),
        ('POST', '/oauth/token', '/oauth/token', {'form': {'grant_type': 'refresh_token',
                                                           'refresh_token': state['grant_e']['refresh_token'], 'client_id': state['client_id']}}),
        ('POST', '/oauth/revoke', '/oauth/revoke', {'form': {'token': state['grant_e']['refresh_token'], 'client_id': state['client_id']}}),
        ('POST', '/mcp', '/mcp', {'json_body': {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, 'headers': {
            'Authorization': 'Bearer ' + state['grant_e']['access_token'], 'MCP-Protocol-Version': PROTOCOL,
            'Accept': 'application/json, text/event-stream'}}),
    ]
    for method, url, template, kwargs in probes:
        status, h, payload, _ = send(method, url, template, **kwargs)
        check(status == 404 and payload == unknown and not h.get_all('Set-Cookie'), ('disabled', url, status, payload))
        for header in ('Content-Type', 'Cache-Control'):
            check(h.get(header) == unknown_headers.get(header), ('disabled', url, header, h.get(header)))
    status, _, listed = request('GET', '/api/v1/mcp/grants', headers={'Cookie': state['owner']})
    check(status == 200 and state['grant_e_id'] in {item['id'] for item in listed['items']}, ('paused grant listed', listed))


def set_installation(owner, enabled, expect_etag):
    """Writes the installation switch through the admin API (ADR 0023, B1b2).
    Returns the new strong ETag. The current row version comes from a read, so
    a stale precondition cannot silently change another administrator's write."""
    status, h, current = request('GET', '/api/v1/admin/mcp-policy', '/api/v1/admin/mcp-policy',
                                 headers={'Cookie': owner})
    check(status == 200 and h['ETag'] == expect_etag, ('installation read', status, current, h.get('ETag')))
    status, h, body = request('PUT', '/api/v1/admin/mcp-policy', '/api/v1/admin/mcp-policy', json_body={
        'enabled': enabled, 'defaultAccountEnabled': current['defaultAccountEnabled'],
        'defaultMaxScopes': current['defaultMaxScopes']},
        headers={'Origin': ORIGIN, 'Cookie': owner, 'If-Match': expect_etag})
    check(status == 200 and body['enabled'] is enabled, ('installation write', enabled, status, body))
    return h['ETag']


def phase_installation(state):
    """The installation switch is live: it pauses every MCP route for the whole
    deployment while the app keeps MCP enabled, and the same token works again
    once it is restored. Runs before the env-off phase, with TENDO_MCP_ENABLED
    still true, so only the database switch differs. A try/finally restores the
    switch even when a check fails, so a failed run cannot leave MCP paused."""
    owner = state['owner']
    token_value = state['grant_e']['access_token']
    etag = set_installation(owner, False, '"1"')
    try:
        # Read after the switch is off: the baseline is the unknown-path 404
        # that an installation-off answer must match, not the enabled one.
        unknown_headers, unknown = unknown_path_404()
        probes = [
            ('POST', '/oauth/register', '/oauth/register', {'json_body': {'redirect_uris': [REDIRECT]}}),
            ('GET', '/.well-known/oauth-protected-resource/mcp', '/.well-known/oauth-protected-resource/mcp', {}),
            ('GET', '/.well-known/oauth-authorization-server', '/.well-known/oauth-authorization-server', {}),
            ('GET', authorize_url(state['client_id'], 'C' * 43, 's'), '/oauth/authorize', {}),
            ('POST', '/oauth/token', '/oauth/token', {'form': {'grant_type': 'refresh_token',
                                                               'refresh_token': state['grant_e']['refresh_token'],
                                                               'client_id': state['client_id']}}),
            ('POST', '/mcp', '/mcp', {'json_body': {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, 'headers': {
                'Authorization': 'Bearer ' + token_value, 'MCP-Protocol-Version': PROTOCOL,
                'Accept': 'application/json, text/event-stream'}}),
        ]
        for method, url, template, kwargs in probes:
            status, h, payload, _ = send(method, url, template, **kwargs)
            check(status == 404 and payload == unknown and not h.get_all('Set-Cookie'), ('installation off', url, status, payload))
            for header in ('Content-Type', 'Cache-Control'):
                check(h.get(header) == unknown_headers.get(header), ('installation off', url, header, h.get(header)))
        status, _, listed = request('GET', '/api/v1/mcp/grants', headers={'Cookie': owner})
        check(status == 200 and state['grant_e_id'] in {item['id'] for item in listed['items']},
              ('connected apps while paused', status, listed))
    except BaseException as probe_error:
        # The probe failure is the one that reports. A restore error is added to
        # it rather than replacing it, so the original cause is never hidden.
        try:
            set_installation(owner, True, etag)
        except Exception as restore_error:
            probe_error.add_note(f'installation restore also failed: {restore_error!r}')
        raise
    set_installation(owner, True, etag)
    check(households(token_value) == {(state['home'], 'MCP Smoke', 'admin')}, 'token after the switch is restored')


def phase_reenabled(state):
    tokens = refresh(state['client_id'], state['grant_e'])
    check(households(tokens['access_token']) == {(state['home'], 'MCP Smoke', 'admin')}, 'grant E households')


def redact(text):
    with open(os.environ['MCP_SMOKE_SECRETS'], encoding='utf-8') as f:
        for value in sorted(filter(None, f.read().splitlines()), key=len, reverse=True):
            text = text.replace(value, '[REDACTED]')
    return text


def main():
    phase, state_path = sys.argv[1], sys.argv[2]
    secret(PASSWORD)
    secret(os.environ['MCP_SMOKE_SETUP_TOKEN'])
    state = json.load(open(state_path)) if os.path.exists(state_path) and os.path.getsize(state_path) else {}
    try:
        {'enabled': phase_enabled, 'installation': phase_installation, 'disabled': phase_disabled,
         'reenabled': phase_reenabled}[phase](state)
    except Exception:
        # Tokens, codes, handles and cookies must not reach the CI log.
        sys.stderr.write(redact(f'MCP smoke {phase} phase failed\n' + traceback.format_exc()))
        sys.exit(1)
    with open(state_path, 'w', encoding='utf-8') as f:
        json.dump(state, f)
    path = os.environ['MCP_SMOKE_FIXTURES']
    existing = json.load(open(path)) if os.path.exists(path) and os.path.getsize(path) else []
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(existing + fixtures, f)


if __name__ == '__main__':
    main()
