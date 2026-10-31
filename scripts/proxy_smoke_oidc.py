#!/usr/bin/env python3
"""Exercise the proxy-backed OIDC link, login, and cookie contracts."""
import http.client, json, os, ssl, urllib.parse

ca = os.environ['OIDC_SMOKE_CA']
context = ssl.create_default_context(cafile=ca)
class VerifiedConnection(http.client.HTTPSConnection):
    def connect(self):
        sock = self._create_connection(('127.0.0.1', self.port), self.timeout, self.source_address)
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)

def record_query_secrets(value):
    try:
        values = urllib.parse.parse_qs(urllib.parse.urlsplit(value).query)
    except ValueError:
        return
    for key in ('state', 'nonce', 'code_challenge', 'code'):
        for secret in values.get(key, []):
            record_secret(secret)

def record_cookie_values(response_headers):
    for set_cookie in response_headers.get_all('Set-Cookie') or []:
        pair = set_cookie.split(';',1)[0]
        if '=' in pair:
            record_secret(pair.split('=',1)[1])

def request(host, port, method, path, headers=None, body=None):
    conn = VerifiedConnection(host, int(port), context=context, timeout=12)
    encoded = json.dumps(body).encode() if body is not None else None
    h = dict(headers or {})
    if body is not None: h['Content-Type'] = 'application/json'
    conn.request(method, path, body=encoded, headers=h)
    r = conn.getresponse(); response_headers = r.headers
    record_cookie_values(response_headers)
    location = response_headers.get('Location', '')
    if location:
        record_query_secrets(location)
    raw = r.read(); conn.close()
    parsed = json.loads(raw) if raw and response_headers.get_content_type() == 'application/json' else raw
    if isinstance(parsed, dict):
        authorization_url = parsed.get('authorizationUrl', '')
        if authorization_url:
            record_query_secrets(authorization_url)
    return r.status, response_headers, parsed

def cookies(headers): return headers.get_all('Set-Cookie') or []
def named_cookie(headers, name):
    found = [v for v in cookies(headers) if v.split(';',1)[0].startswith(name+'=')]
    pairs = [v.split(';',1)[0] for v in found]
    for pair in pairs:
        if '=' in pair:
            record_secret(pair.split('=',1)[1])
    assert len(found) == 1, f"expected one {name} cookie, got {[v.split(';',1)[0].split('=',1)[0] for v in cookies(headers)]}"
    return pairs[0]
def cookie_attrs(cookie):
    return [x.strip().lower() for x in cookie.split(';')[1:]]
def secure_session(headers):
    value = named_cookie(headers, '__Host-tendo_session')
    cookie = next(v for v in cookies(headers) if v.startswith(value+';'))
    attrs = cookie_attrs(cookie)
    assert 'secure' in attrs and 'httponly' in attrs and 'path=/' in attrs and 'samesite=lax' in attrs and not any(x.startswith('domain=') for x in attrs), f"unexpected session cookie attribute names: {[x.split('=',1)[0] for x in attrs]}"
    return value

def start(purpose, session=None, password=None, forged=False):
    b = {'purpose': purpose}
    if password is not None: b['currentPassword'] = password
    hs = {'Origin':'https://tendo.test'}
    if session: hs['Cookie'] = session
    if forged: hs.update({'X-Forwarded-Host':'attacker.test','X-Forwarded-Proto':'http','Forwarded':'host=attacker.test;proto=http'})
    status, h, data = request('tendo.test', os.environ['OIDC_PROXY_PORT'], 'POST', '/api/v1/auth/oidc/start', hs, b)
    assert status == 200, f"OIDC start returned HTTP {status}"
    url = urllib.parse.urlsplit(data['authorizationUrl'])
    assert url.scheme == 'https' and url.netloc == 'oidc.test:8443', f"unexpected authorization endpoint: {url.scheme}://{url.netloc}"
    query = urllib.parse.parse_qs(url.query, strict_parsing=True)
    assert query['redirect_uri'] == ['https://tendo.test/api/v1/auth/oidc/callback'], "authorization redirect_uri was not canonical"
    flow = named_cookie(h, '__Host-tendo_oidc')
    fc = next(v for v in cookies(h) if v.startswith(flow+';'))
    attrs = cookie_attrs(fc)
    assert 'secure' in attrs and 'httponly' in attrs and 'path=/' in attrs and 'samesite=lax' in attrs and not any(x.startswith('domain=') for x in attrs), f"unexpected flow cookie attribute names: {[x.split('=',1)[0] for x in attrs]}"
    record_secret(flow.split('=',1)[1])
    return query, flow

def authorize(query, sub='owner-subject'):
    q = dict(query); q['sub'] = [sub]
    path = '/authorize?' + urllib.parse.urlencode(q, doseq=True)
    status,h,_ = request('oidc.test',os.environ['OIDC_PROVIDER_PORT'],'GET',path)
    location = h.get('Location','')
    parsed = urllib.parse.urlsplit(location)
    assert status == 302 and parsed.scheme == 'https' and parsed.netloc == 'tendo.test' and parsed.path == '/api/v1/auth/oidc/callback', f"provider authorize returned HTTP {status} with callback target {parsed.scheme}://{parsed.netloc}{parsed.path}"
    return parsed

def callback(parsed, flow, session=None, flow_cookie_name='__Host-tendo_oidc'):
    hs={'Cookie':flow_cookie_name+'='+flow.split('=',1)[1]+('; '+session if session else '')}
    return request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET',parsed.path+'?'+parsed.query,hs)

def record_secret(value):
    os.chmod(os.environ['OIDC_SECRET_VALUES'],0o600)
    with open(os.environ['OIDC_SECRET_VALUES'],'a',encoding='utf-8') as f:
        f.write(value+'\n'); f.flush()
s,h,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/auth/oidc')
assert s == 200 and b == {'enabled':True,'displayName':'Proxy Smoke Provider'}, f"OIDC discovery returned HTTP {s}"
old = os.environ['OIDC_SESSION_COOKIE']
q,flow = start('link',old,'correct horse battery',True)
parsed = authorize(q)
s,h,b = callback(parsed,flow,old)
assert s == 303 and h.get('Location') == '/account#oidc=connected' and h.get('Cache-Control') == 'no-store', f"link callback returned HTTP {s} with Location {urllib.parse.urlsplit(h.get('Location','')).path}"
cleared = [v for v in cookies(h) if v.split(';',1)[0].startswith('__Host-tendo_oidc=') and 'Max-Age=0' in v]
assert len(cleared) == 1, f"expected one cleared __Host-tendo_oidc cookie, got {[v.split(';',1)[0].split('=',1)[0] for v in cookies(h)]}"
new = secure_session(h)
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':old})
assert s == 401, f"old session returned HTTP {s}"
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':new})
assert s == 200 and b['login']=='proxy_owner',f"session check returned HTTP {s}"
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/auth/oidc/identity',{'Cookie':new})
assert s == 200 and b == {'linked':True},f"OIDC identity check returned HTTP {s}"
s,_,_ = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'DELETE','/api/v1/session',{'Origin':'https://tendo.test','Cookie':new})
assert s == 204, f"logout returned HTTP {s}"
q,flow = start('login'); parsed = authorize(q); s,h,b = callback(parsed,flow)
assert s == 303 and h.get('Location') == '/', f"login callback returned HTTP {s} with Location {urllib.parse.urlsplit(h.get('Location','')).path}"
login_cookie = secure_session(h)
s,_,b = request('tendo.test',os.environ['OIDC_PROXY_PORT'],'GET','/api/v1/session',{'Cookie':login_cookie})
assert s == 200 and b['login']=='proxy_owner',f"session check returned HTTP {s}"
q,flow = start('login'); parsed = authorize(q); s,h,b = callback(parsed,flow,flow_cookie_name='tendo_oidc')
assert s == 303 and h.get('Location') == '/login#oidcError=invalid_flow', f"invalid cookie callback returned HTTP {s} with Location {urllib.parse.urlsplit(h.get('Location','')).path}"
assert not any(v.split(';',1)[0].startswith('__Host-tendo_session=') for v in cookies(h)), f"unexpected session cookie names: {[v.split(';',1)[0].split('=',1)[0] for v in cookies(h)]}"
print('HTTPS proxy OIDC link/login/cookie checks passed')
