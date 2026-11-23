#!/usr/bin/env python3
"""Drive the live MCP OAuth smoke against a Compose app (V5-12 A4c, ADR 0023).

Usage: mcp_smoke_driver.py <phase> <state.json>, phase is one of enabled,
disabled, reenabled. Environment: MCP_SMOKE_ORIGIN, MCP_SMOKE_SETUP_TOKEN,
MCP_SMOKE_PSQL (a psql command line), MCP_SMOKE_FIXTURES, MCP_SMOKE_SECRETS.

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
import base64, hashlib, json, os, secrets as rnd, subprocess, sys
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


def secret(value):
    if value:
        with open(os.environ['MCP_SMOKE_SECRETS'], 'a', encoding='utf-8') as f:
            f.write(value + '\n')


def request(method, url, template=None, json_body=None, form=None, raw=None, headers=None):
    """Send one request. A template records the response as a fixture."""
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
    media = (h.get('Content-Type') or '').split(';')[0].strip()
    body = None
    if payload:
        body = payload.decode() if media == 'text/plain' else json.loads(payload)
    if template:
        fixture = {
            'path': template, 'url': url.split('?', 1)[0], 'method': method.lower(), 'status': status,
            'headers': {k.lower(): (h.get_all(k) if k.lower() == 'set-cookie' else h.get_all(k)[0]) for k in h.keys()},
        }
        if payload:
            fixture['body'] = body
        fixtures.append(fixture)
    return status, h, body


def psql(sql):
    out = subprocess.check_output(os.environ['MCP_SMOKE_PSQL'].split() + ['-c', sql], text=True)
    return out.strip().splitlines()[-1] if out.strip() else ''


def cookie_pair(headers, name):
    for value in headers.get_all('Set-Cookie') or []:
        pair = value.split(';', 1)[0]
        if pair.startswith(name + '=') and pair.split('=', 1)[1]:
            return pair
    raise AssertionError(('missing cookie', name, headers.get_all('Set-Cookie')))


def login(user):
    status, h, body = request('POST', '/api/v1/session', json_body={'login': user, 'password': PASSWORD},
                              headers={'Origin': ORIGIN})
    assert status == 201, ('login', user, status, body)
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
    assert status == 303 and body is None and location.startswith('/connect#request='), ('authorize', status, location)
    handle = location.split('=', 1)[1]
    secret(handle)
    binding = cookie_pair(h, 'tendo_mcp_consent')
    return {'verifier': verifier, 'state': state, 'handle': handle, 'binding': binding}


def decide(flow, session, decision, expect=200):
    url = f"/api/v1/mcp/authorization-requests/{flow['handle']}/decision"
    status, h, body = request('POST', url, DECISION, json_body={'decision': decision},
                              headers={'Origin': ORIGIN, 'Cookie': session + '; ' + flow['binding']})
    assert status == expect, ('decision', decision, status, body)
    if status != 200:
        return body
    target = urllib.parse.urlsplit(body['redirectTo'])
    assert f'{target.scheme}://{target.netloc}{target.path}' == REDIRECT, body
    query = urllib.parse.parse_qs(target.query, strict_parsing=True)
    assert query['state'] == [flow['state']] and query['iss'] == [ORIGIN], query
    if decision == 'approve':
        assert set(query) == {'code', 'state', 'iss'}, query
        secret(query['code'][0])
        return query['code'][0]
    assert set(query) == {'error', 'state', 'iss'} and query['error'] == ['access_denied'], query
    return None


def token(form, expect=200, error=None):
    status, _, body = request('POST', '/oauth/token', '/oauth/token', form=form)
    assert status == expect, ('token', form.get('grant_type'), status, body)
    if error:
        assert body == {'error': error, 'error_description': body.get('error_description')}, body
    if status == 200:
        assert body['token_type'] == 'Bearer' and 0 < body['expires_in'] <= 600, body
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
    assert status == expect, ('mcp', message, status, body)
    return h, body


def households(access):
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call',
                           'params': {'name': 'list_households', 'arguments': {}}})
    result = body['result']
    assert result.get('isError') is not True, body
    return {(row['id'], row['name'], row['role']) for row in result['structuredContent']['households']}


def mcp_refused(access):
    h, body = mcp(access, {'jsonrpc': '2.0', 'id': 9, 'method': 'tools/list'}, expect=401)
    assert body['error'] == 'invalid_token' and 'error="invalid_token"' in h['WWW-Authenticate'], (h, body)


def grant_of(access):
    digest = hashlib.sha256(access.encode()).hexdigest()
    return psql(f"SELECT grant_id FROM mcp_access_tokens WHERE token_hash=decode('{digest}','hex')")


def unknown_path_404():
    status, _, body = request('GET', '/api/v1/no-such-route')
    assert status == 404, (status, body)
    return body


def phase_enabled(state):
    status, _, body = request('PUT', '/api/v1/auth/setup', json_body={
        'login': 'mcp_owner', 'password': PASSWORD, 'householdName': 'MCP Smoke', 'timezone': 'UTC'},
        headers={'Origin': ORIGIN, 'X-Tendo-Setup-Token': os.environ['MCP_SMOKE_SETUP_TOKEN']})
    assert status == 201, ('setup', status, body)
    owner = login('mcp_owner')
    secret(owner.split('=', 1)[1])
    _, _, me = request('GET', '/api/v1/session', headers={'Cookie': owner})
    home = me['defaultHouseholdId']

    # Discovery.
    status, _, prm = request('GET', '/.well-known/oauth-protected-resource/mcp', '/.well-known/oauth-protected-resource/mcp')
    assert status == 200 and prm['resource'] == RESOURCE and prm['authorization_servers'] == [ORIGIN], prm
    status, _, asm = request('GET', '/.well-known/oauth-authorization-server', '/.well-known/oauth-authorization-server')
    assert status == 200 and asm['issuer'] == ORIGIN, asm
    for key, path in [('authorization_endpoint', '/oauth/authorize'), ('token_endpoint', '/oauth/token'),
                      ('registration_endpoint', '/oauth/register'), ('revocation_endpoint', '/oauth/revoke')]:
        assert asm[key] == ORIGIN + path, (key, asm)
    assert asm['code_challenge_methods_supported'] == ['S256'], asm

    # Registration: one client for the whole smoke (10 per minute per IP).
    status, _, refused = request('POST', '/oauth/register', '/oauth/register',
                                 json_body={'redirect_uris': ['javascript:alert(1)'], 'client_name': 'Refused'})
    assert status == 400 and refused['error'] == 'invalid_redirect_uri', refused
    status, _, client = request('POST', '/oauth/register', '/oauth/register',
                                json_body={'redirect_uris': [REDIRECT], 'client_name': 'MCP Smoke Client'})
    assert status == 201 and client['token_endpoint_auth_method'] == 'none', client
    assert client['scope'] == 'tendo:read tendo:write' and client['redirect_uris'] == [REDIRECT], client
    cid = client['client_id']

    # Authorize refusals redirect to /connect#error= and set no cookie.
    _, challenge = pkce()
    for label, url, code in [
            ('plain PKCE', authorize_url(cid, challenge, 's', method='plain'), 'invalid_request'),
            ('foreign resource', authorize_url(cid, challenge, 's', resource='http://evil.invalid/mcp'), 'invalid_target')]:
        status, h, body = request('GET', url, '/oauth/authorize')
        assert status == 303 and h['Location'] == '/connect#error=' + code, (label, status, h['Location'])
        assert not h.get_all('Set-Cookie'), (label, h.get_all('Set-Cookie'))

    # Grant A: consent reads, decision refusals, verifier mismatch and code reuse.
    flow = begin(cid)
    consent_url = f"/api/v1/mcp/authorization-requests/{flow['handle']}"
    status, _, body = request('GET', consent_url, CONSENT, headers={'Cookie': flow['binding']})
    assert status == 401 and body['code'] == 'unauthenticated', ('consent without session', status, body)
    status, _, body = request('GET', consent_url, CONSENT, headers={'Cookie': owner})
    assert status == 404 and body['code'] == 'authorization_request_not_found', ('consent without binding', status, body)
    status, _, view = request('GET', consent_url, CONSENT, headers={'Cookie': owner + '; ' + flow['binding']})
    assert status == 200 and view['clientName'] == 'MCP Smoke Client' and view['clientNameVerified'] is False, view
    assert view['redirectUri'] == REDIRECT and sorted(view['scopes']) == ['tendo:read', 'tendo:write'], view
    status, _, body = request('POST', consent_url + '/decision', DECISION, json_body={'decision': 'approve'},
                              headers={'Origin': FOREIGN, 'Cookie': owner + '; ' + flow['binding']})
    assert status == 403, ('decision foreign Origin', status, body)
    status, _, body = request('POST', consent_url + '/decision', DECISION, json_body={'decision': 'maybe'},
                              headers={'Origin': ORIGIN, 'Cookie': owner + '; ' + flow['binding']})
    assert status == 400 and body['code'] == 'invalid_request', ('decision value', status, body)
    code = decide(flow, owner, 'approve')
    replay = decide(flow, owner, 'approve', expect=404)
    assert replay['code'] == 'authorization_request_not_found', replay
    token({'grant_type': 'authorization_code', 'code': code, 'redirect_uri': REDIRECT, 'client_id': cid,
           'code_verifier': flow['verifier'], 'resource': 'http://evil.invalid/mcp'}, 400, 'invalid_target')
    wrong, _ = pkce()
    exchange(cid, flow, code, 400, 'invalid_grant', verifier=wrong)
    grant_a = exchange(cid, flow, code)
    exchange(cid, flow, code, 400, 'invalid_grant')
    mcp_refused(grant_a['access_token'])
    refresh(cid, grant_a, 400, 'invalid_grant')

    # Denial issues no code and creates no grant.
    before = psql('SELECT count(*) FROM mcp_grants')
    assert decide(begin(cid), owner, 'deny') is None
    assert psql('SELECT count(*) FROM mcp_grants') == before, 'denial created a grant'

    # Grant B: refresh rotation, then replay of the consumed refresh token.
    grant_b = connect(cid, owner)
    rotated = refresh(cid, grant_b)
    assert rotated['refresh_token'] != grant_b['refresh_token'], 'refresh token not rotated'
    assert households(rotated['access_token']) == {(home, 'MCP Smoke', 'admin')}
    refresh(cid, grant_b, 400, 'invalid_grant')
    refresh(cid, rotated, 400, 'invalid_grant')
    mcp_refused(rotated['access_token'])

    # Grant C: the /mcp protocol surface, then client revocation.
    grant_c = connect(cid, owner)
    access = grant_c['access_token']
    h, body = mcp(None, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, expect=401)
    challenge = h['WWW-Authenticate']
    assert 'error=' not in challenge and f'resource_metadata="{ORIGIN}/.well-known/oauth-protected-resource/mcp"' in challenge, challenge
    mcp_refused('A' * 43)
    mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, origin=FOREIGN, expect=403)
    _, body = mcp(access, None, raw=b'[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]', expect=400)
    assert body['error']['code'] == -32600, body
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, version=None, expect=400)
    assert body['error']['code'] == -32600, body
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {
        'protocolVersion': PROTOCOL, 'capabilities': {}, 'clientInfo': {'name': 'mcp-smoke', 'version': '1'}}}, version=None)
    assert body['result']['protocolVersion'] == PROTOCOL, body
    mcp(access, {'jsonrpc': '2.0', 'method': 'notifications/initialized'}, expect=202)
    _, body = mcp(access, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'})
    assert [tool['name'] for tool in body['result']['tools']] == ['list_households'], body
    assert households(access) == {(home, 'MCP Smoke', 'admin')}
    status, _, body = request('POST', '/oauth/revoke', '/oauth/revoke', form={'client_id': cid})
    assert status == 400 and body['error'] == 'invalid_request', ('revoke without token', status, body)
    status, _, body = request('POST', '/oauth/revoke', '/oauth/revoke', form={'token': grant_c['refresh_token'], 'client_id': cid})
    assert status == 200 and body is None, ('revoke', status, body)
    mcp_refused(access)
    refresh(cid, grant_c, 400, 'invalid_grant')

    # Grant W: a tendo:write-only grant holds no scope any tool requires.
    grant_w = connect(cid, owner, scope='tendo:write')
    assert grant_w['scope'] == 'tendo:write', grant_w
    h, body = mcp(grant_w['access_token'], {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}, expect=403)
    assert body['error'] == 'insufficient_scope' and 'error="insufficient_scope"' in h['WWW-Authenticate'], (h, body)

    # Grant D: disconnect through Connected apps reaches the next request.
    grant_d = connect(cid, owner)
    grant_d_id = grant_of(grant_d['access_token'])
    status, _, listed = request('GET', '/api/v1/mcp/grants', headers={'Cookie': owner})
    assert status == 200 and grant_d_id in {item['id'] for item in listed['items']}, listed
    status, _, body = request('DELETE', '/api/v1/mcp/grants/' + grant_d_id, headers={'Origin': ORIGIN, 'Cookie': owner})
    assert status == 204, ('disconnect', status, body)
    mcp_refused(grant_d['access_token'])
    refresh(cid, grant_d, 400, 'invalid_grant')

    # Grant M: a second account in a household the owner does not belong to.
    status, _, invite = request('POST', f'/api/v1/households/{home}/invitations', json_body={},
                                headers={'Origin': ORIGIN, 'Cookie': owner, 'Idempotency-Key': 'mcp-smoke-member'})
    assert status == 201, ('invitation', status, invite)
    secret(invite['token'])
    status, _, body = request('POST', '/api/v1/auth/invitations/accept', json_body={
        'token': invite['token'], 'login': 'mcp_member', 'password': PASSWORD}, headers={'Origin': ORIGIN})
    assert status == 201, ('accept', status, body)
    member = login('mcp_member')
    secret(member.split('=', 1)[1])
    _, _, member_me = request('GET', '/api/v1/session', headers={'Cookie': member})
    status, _, other = request('POST', '/api/v1/admin/households', json_body={
        'name': 'Member Only', 'timezone': 'UTC', 'adminUserId': member_me['userId']},
        headers={'Origin': ORIGIN, 'Cookie': owner, 'Idempotency-Key': 'mcp-smoke-other'})
    assert status == 201, ('member household', status, other)
    grant_m = connect(cid, member)
    assert households(grant_m['access_token']) == {(home, 'MCP Smoke', 'member'), (other['id'], 'Member Only', 'admin')}
    owner_check = connect(cid, owner)
    assert households(owner_check['access_token']) == {(home, 'MCP Smoke', 'admin')}, 'owner sees a foreign household'

    # Grant E survives the switch-off.
    grant_e = connect(cid, owner)
    state.update(client_id=cid, owner=owner, home=home, grant_e=grant_e, grant_e_id=grant_of(grant_e['access_token']))

    reasons = psql("SELECT string_agg(DISTINCT reason, ',' ORDER BY reason) FROM mcp_grant_events WHERE event='revoked'")
    assert reasons == 'client,code_reuse,replay,user', reasons


def phase_disabled(state):
    unknown = unknown_path_404()
    flow_handle = 'H' * 43
    probes = [
        ('POST', '/oauth/register', '/oauth/register', {'json_body': {'redirect_uris': [REDIRECT]}}),
        ('GET', '/.well-known/oauth-protected-resource/mcp', '/.well-known/oauth-protected-resource/mcp', {}),
        ('GET', '/.well-known/oauth-authorization-server', '/.well-known/oauth-authorization-server', {}),
        ('GET', authorize_url(state['client_id'], 'C' * 43, 's'), '/oauth/authorize', {}),
        ('GET', f'/api/v1/mcp/authorization-requests/{flow_handle}', CONSENT, {'headers': {'Cookie': state['owner']}}),
        # Asserted but not recorded: the contract declares the consent cookie
        # clear on every decision 404, while the disabled surface answers the
        # byte-identical unknown-path 404 with no cookie. The enabled 404s are
        # recorded above.
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
        status, h, body = request(method, url, template, **kwargs)
        assert status == 404 and body == unknown and not h.get_all('Set-Cookie'), ('disabled', url, status, body)
    status, _, listed = request('GET', '/api/v1/mcp/grants', headers={'Cookie': state['owner']})
    assert status == 200 and state['grant_e_id'] in {item['id'] for item in listed['items']}, ('paused grant listed', listed)


def phase_reenabled(state):
    tokens = refresh(state['client_id'], state['grant_e'])
    assert households(tokens['access_token']) == {(state['home'], 'MCP Smoke', 'admin')}


def main():
    phase, state_path = sys.argv[1], sys.argv[2]
    state = json.load(open(state_path)) if os.path.exists(state_path) and os.path.getsize(state_path) else {}
    {'enabled': phase_enabled, 'disabled': phase_disabled, 'reenabled': phase_reenabled}[phase](state)
    with open(state_path, 'w', encoding='utf-8') as f:
        json.dump(state, f)
    path = os.environ['MCP_SMOKE_FIXTURES']
    existing = json.load(open(path)) if os.path.exists(path) and os.path.getsize(path) else []
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(existing + fixtures, f)


if __name__ == '__main__':
    main()
