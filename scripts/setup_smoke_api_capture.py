"""Capture and validate live API responses for setup-smoke.sh."""

import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

COVERAGE_SCOPE = ','.join((
    'getInitialSetup', 'createInitialOwner', 'createSession', 'getSession',
    'deleteSession', 'createInvitation', 'listInvitations', 'revokeInvitation',
    'acceptInvitationWithNewAccount', 'acceptInvitation', 'listMembers',
    'getHousehold', 'createSubject', 'listSubjects', 'getSubject',
    'updateSubject', 'createItem', 'listItems', 'createItemCompletion',
    'listItemCompletions', 'undoItemCompletion', 'getItemsSummary', 'getItem',
    'updateItem',
))


def request(origin, method, url, body=None, headers=None):
    request_headers = {'Content-Type': 'application/json', **(headers or {})}
    data = body if isinstance(body, bytes) else (
        json.dumps(body, ensure_ascii=False).encode() if body is not None else None
    )
    request = urllib.request.Request(
        origin + url, data=data, headers=request_headers, method=method
    )
    try:
        with urllib.request.urlopen(request, timeout=8) as response:
            status, response_headers, raw = response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        status, response_headers, raw = error.code, error.headers, error.read()
    result = json.loads(raw) if raw else None
    return status, response_headers, result


def capture_initial(origin, token, output):
    fixtures = []

    def capture(method, url, body=None, headers=None):
        status, response_headers, result = request(origin, method, url, body, headers)
        if url in ('/api/v1/auth/setup', '/api/v1/session') and status != 409:
            fixture = {
                'path': url, 'method': method.lower(), 'status': status,
                'headers': dict(response_headers.items()),
            }
            if result is not None:
                fixture['body'] = result
            fixtures.append(fixture)
        return status, response_headers, result

    status, headers, body = capture('GET', '/api/v1/auth/setup')
    assert status == 200 and body == {'required': True}
    assert headers['ETag'] == '"setup-v1-required"'
    cases = (
        ('GET', '/api/v1/auth/oidc', None, None, 200, 'status'),
        ('GET', '/api/v1/auth/oidc', None, {'Origin': 'http://foreign.example'}, 403, 'status'),
        ('GET', '/api/v1/auth/oidc/identity', None, None, 404, 'identity'),
        ('POST', '/api/v1/auth/oidc/start', {'purpose': 'login'}, {'Origin': origin}, 404, 'start'),
        ('GET', '/api/v1/auth/setup', None, {'Origin': 'http://foreign.example'}, 403, 'setup origin'),
    )
    for method, url, payload, headers, expected, label in cases:
        status, response_headers, result = request(origin, method, url, payload, headers)
        assert status == expected, (label, status, result)
        if label == 'status' and status == 200:
            assert result == {'enabled': False}, (label, status, result)
        elif label in ('identity', 'start'):
            assert result['code'] == 'oidc_disabled', (label, status, result)
        fixtures.append({
            'path': url, 'method': method.lower(), 'status': status,
            'headers': dict(response_headers.items()), 'body': result,
        })
    valid = {
        'login': 'owner_smoke', 'password': 'correct horse battery',
        'householdName': 'Veselí 家族', 'timezone': 'Europe/Prague',
    }
    checks = (
        ('PUT', valid, {'Origin': origin, 'X-Tendo-Setup-Token': 'wrong'}, 401),
        ('PUT', valid, {'X-Tendo-Setup-Token': token}, 403),
        ('PUT', b'x' * 8193, {'Origin': origin, 'X-Tendo-Setup-Token': token}, 413),
        ('PUT', valid, {'Origin': 'http://foreign.example', 'X-Tendo-Setup-Token': token}, 403),
    )
    for method, payload, headers, expected in checks:
        status, _, _ = capture(method, '/api/v1/auth/setup', payload, headers)
        assert status == expected, ('setup rejection', status)
    status, _, body = capture('GET', '/api/v1/auth/setup')
    assert status == 200 and body == {'required': True}
    status, _, _ = capture(
        'PUT', '/api/v1/auth/setup', b'{}',
        {'Origin': origin, 'X-Tendo-Setup-Token': token, 'Content-Type': 'text/plain'},
    )
    assert status == 415
    invalid = {**valid, 'timezone': 'Not/A_Timezone'}
    status, _, _ = capture(
        'PUT', '/api/v1/auth/setup', invalid,
        {'Origin': origin, 'X-Tendo-Setup-Token': token},
    )
    assert status == 422
    status, _, body = capture('GET', '/api/v1/auth/setup')
    assert status == 200 and body == {'required': True}
    status, headers, body = capture(
        'PUT', '/api/v1/auth/setup', valid,
        {'Origin': origin, 'X-Tendo-Setup-Token': token},
    )
    assert status == 201 and body == {'required': False}
    fixtures.append({
        'path': '/api/v1/auth/setup', 'method': 'put', 'status': status,
        'headers': dict(headers.items()), 'body': body,
    })
    status, _, body = capture('GET', '/api/v1/auth/setup')
    assert status == 200 and body == {'required': False}
    status, _, _ = capture(
        'PUT', '/api/v1/auth/setup', valid,
        {'Origin': origin, 'X-Tendo-Setup-Token': token},
    )
    assert status == 429
    with open(output, 'w', encoding='utf-8') as stream:
        json.dump(fixtures, stream, ensure_ascii=False)


def assert_duplicate_setup(origin, token):
    body = {
        'login': 'owner_smoke', 'password': 'correct horse battery',
        'householdName': 'Veselí 家族', 'timezone': 'Europe/Prague',
    }
    request = urllib.request.Request(
        origin + '/api/v1/auth/setup',
        data=json.dumps(body, ensure_ascii=False).encode(),
        headers={
            'Content-Type': 'application/json', 'Origin': origin,
            'X-Tendo-Setup-Token': token,
        },
        method='PUT',
    )
    try:
        urllib.request.urlopen(request, timeout=8)
        raise AssertionError('duplicate setup unexpectedly succeeded')
    except urllib.error.HTTPError as error:
        assert error.code == 409, ('duplicate setup after restart', error.code)


def validate_union(root, fixture_paths, output_path):
    for path in fixture_paths:
        subprocess.run([
            'node', f'{root}/api/check-health.mjs', path
        ], check=True)
    merged = []
    for path in fixture_paths:
        with open(path, encoding='utf-8') as stream:
            merged.extend(json.load(stream))
    with open(output_path, 'w', encoding='utf-8') as stream:
        json.dump(merged, stream)
    environment = os.environ.copy()
    environment['API_RESPONSE_FIXTURES'] = output_path
    environment['API_REQUIRE_OPERATION_COVERAGE'] = '1'
    environment['API_OPERATION_COVERAGE_SCOPE'] = COVERAGE_SCOPE
    subprocess.run(['node', f'{root}/api/check-health.mjs'], check=True, env=environment)


if __name__ == '__main__':
    if sys.argv[1] == 'initial':
        capture_initial(sys.argv[2], sys.argv[3], sys.argv[4])
    elif sys.argv[1] == 'validate':
        validate_union(sys.argv[2], sys.argv[3:6], sys.argv[6])
    elif sys.argv[1] == 'duplicate-setup':
        assert_duplicate_setup(sys.argv[2], sys.argv[3])
    else:
        raise SystemExit(f'unknown action: {sys.argv[1]}')
