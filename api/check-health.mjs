import assert from 'node:assert/strict'
import Ajv2020 from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'
import { parse } from 'yaml'
import { readFile } from 'node:fs/promises'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const ajv = new Ajv2020({ strict: false })
addFormats(ajv)
for (const [name, schema] of Object.entries(contract.components.schemas)) ajv.addSchema(schema, `#/components/schemas/${name}`)
const schemas = Object.fromEntries(Object.entries(contract.components.schemas).map(([name, schema]) => [name, ajv.compile(schema)]))

function assertBody(path, status, body, method) {
  const responseSpec = contract.paths[path][method]?.responses[String(status)]
  assert.ok(responseSpec, `${path}: undocumented HTTP ${status}`)
  if (!responseSpec.content) {
    assert.equal(body, undefined, `${path} HTTP ${status} must have an empty body`)
    return
  }
  const media = responseSpec.content['application/json'] ? 'application/json' : 'application/problem+json'
  const schema = responseSpec.content[media]?.schema
  assert.ok(schema, `${path}: response schema missing`)
  const validate = ajv.compile(schema)
  assert.ok(validate(body), `${path} HTTP ${status} body invalid: ${JSON.stringify(validate.errors)}`)
}

function cookieValues(value) {
  if (Array.isArray(value)) return value
  if (typeof value === 'string') return [value]
  return []
}

function assertFlowCookie(path, status, setCookie, clearing = false) {
  assert.equal(typeof setCookie, 'string', `${path} HTTP ${status} missing Set-Cookie`)
  const parts = setCookie.split(';').map(part => part.trim())
  const pair = parts.shift() ?? ''
  const separator = pair.indexOf('=')
  assert.ok(separator > 0, `${path} HTTP ${status} malformed flow cookie`)
  const name = pair.slice(0, separator)
  const value = pair.slice(separator + 1)
  assert.ok(['tendo_oidc', '__Host-tendo_oidc'].includes(name), `${path} HTTP ${status} unexpected flow cookie name`)
  const attributes = parts.map(attribute => attribute.toLowerCase())
  const requireAttribute = attribute => assert.ok(attributes.includes(attribute), `${path} flow cookie missing ${attribute}`)
  requireAttribute('path=/')
  requireAttribute('httponly')
  requireAttribute('samesite=lax')
  assert.ok(!attributes.some(attribute => attribute.startsWith('domain=')), `${path} flow cookie must not set Domain`)
  if (name.startsWith('__Host-')) requireAttribute('secure')
  const maxAge = attributes.find(attribute => attribute.startsWith('max-age='))
  assert.ok(maxAge, `${path} flow cookie missing Max-Age`)
  if (clearing) {
    assert.equal(value, '', `${path} flow clear must have an empty value`)
    assert.ok(['max-age=0', 'max-age=-1'].includes(maxAge), `${path} flow clear must have a clearing Max-Age`)
  } else {
    assert.match(value, /^[A-Za-z0-9_-]{43}$/, `${path} flow cookie value must be 43 base64url characters`)
    assert.equal(maxAge, 'max-age=600', `${path} flow cookie must have Max-Age=600`)
    const expires = attributes.find(attribute => attribute.startsWith('expires='))
    if (expires) assert.ok(Number.isFinite(Date.parse(expires.slice('expires='.length))), `${path} flow cookie has invalid Expires`)
  }
}

function assertSessionCookie(path, status, setCookie) {
  if (Array.isArray(setCookie)) {
    assert.equal(setCookie.length, 1, `${path} HTTP ${status} expected one session cookie`)
    setCookie = setCookie[0]
  }
  assert.equal(typeof setCookie, 'string', `${path} HTTP ${status} missing Set-Cookie`)
  const [pair, ...attributes] = setCookie.split(';').map(part => part.trim())
  const [name, value = ''] = [pair.slice(0, pair.indexOf('=')), pair.slice(pair.indexOf('=') + 1)]
  assert.ok(['tendo_session', '__Host-tendo_session'].includes(name), `${path} HTTP ${status} unexpected cookie name ${name}`)
  const lower = attributes.map(attribute => attribute.toLowerCase())
  assert.ok(lower.includes('httponly'), `${path} HTTP ${status} cookie must be HttpOnly`)
  assert.ok(lower.includes('samesite=lax'), `${path} HTTP ${status} cookie must be SameSite=Lax`)
  assert.ok(lower.includes('path=/'), `${path} HTTP ${status} cookie must have Path=/`)
  assert.ok(!lower.some(attribute => attribute.startsWith('domain=')), `${path} HTTP ${status} cookie must not set Domain`)
  if (name.startsWith('__Host-')) assert.ok(lower.includes('secure'), `${path} HTTP ${status} __Host- cookie must be Secure`)
  if (Number(status) !== 201) {
    assert.equal(value, '', `${path} HTTP ${status} clearing cookie must be empty`)
    assert.ok(lower.includes('max-age=0'), `${path} HTTP ${status} clearing cookie must have Max-Age=0`)
  } else {
    assert.match(value, /^[A-Za-z0-9_-]{43}$/, `${path} HTTP ${status} cookie value must be 43 base64url characters`)
    assert.ok(lower.includes('max-age=2592000'), `${path} HTTP ${status} cookie must have a 30-day Max-Age`)
  }
}

function assertResponse(path, status, body, method, headers = {}) {
  const normalizedHeaders = Object.fromEntries(Object.entries(headers).map(([key,value])=>[key.toLowerCase(),value]))
  headers = normalizedHeaders
  const responseSpec = contract.paths[path][method]?.responses[String(status)]
  assert.ok(responseSpec, `${path}: undocumented HTTP ${status}`)
  const expectedMedia = Object.keys(responseSpec.content ?? {})
  if (expectedMedia.length) {
    const actualMedia = String(headers['content-type'] ?? '').split(';')[0].trim().toLowerCase()
    assert.ok(expectedMedia.includes(actualMedia), `${path} HTTP ${status} wrong media type: ${actualMedia || '(missing)'}`)
  }
  const requestId = headers['x-request-id']
  if (responseSpec.headers?.['X-Request-ID']) assert.ok(/^[A-Za-z0-9._-]{1,64}$/.test(requestId ?? ''), `${path} HTTP ${status} invalid request ID ${requestId ?? '(missing)'}`)
  if (responseSpec.headers?.['Cache-Control']) assert.equal(headers['cache-control'], 'no-store', `${path} HTTP ${status} cache-control must be no-store`)
  if (responseSpec.headers?.ETag) {
    assert.ok(typeof headers.etag === 'string' && headers.etag.length > 0, `${path} HTTP ${status} missing ETag`)
    const etagPattern = responseSpec.headers.ETag.schema?.pattern
    if (etagPattern) assert.match(headers.etag, new RegExp(etagPattern), `${path} HTTP ${status} ETag does not match its documented pattern`)
  }
  const cookieHeader = responseSpec.headers?.['Set-Cookie']
  const cookieRef = cookieHeader?.$ref
  if (cookieRef && path === '/api/v1/auth/oidc/identity') {
    // On authenticated routes this header describes a stale session clear; a valid
    // session response must not emit any cookie.
    if (headers['set-cookie'] !== undefined) assertSessionCookie(path, status, headers['set-cookie'])
  } else if (cookieRef) {
    const cookieSpec = contract.components.headers[cookieRef.split('/').pop()]
    if (cookieSpec.required === true || headers['set-cookie'] !== undefined) assertSessionCookie(path, status, headers['set-cookie'])
  } else if (path === '/api/v1/auth/oidc/start' && status === 200) {
    assert.ok(cookieHeader, `${path} HTTP ${status} missing Set-Cookie contract declaration`)
    const cookies = cookieValues(headers['set-cookie'])
    assert.equal(cookies.length, 1, `${path} HTTP ${status} must set exactly one flow cookie`)
    assertFlowCookie(path,status,cookies[0],false)
  } else if (cookieHeader && path === '/api/v1/auth/oidc/start') {
    const cookies = cookieValues(headers['set-cookie'])
    assert.equal(cookies.length, 1, `${path} HTTP ${status} must set exactly one flow cookie`)
    assertFlowCookie(path,status,cookies[0],false)
  } else if (cookieHeader && path === '/api/v1/auth/oidc/callback') {
    const cookies = cookieValues(headers['set-cookie'])
    assert.ok(cookies.length >= 1 && cookies.length <= 2, `${path} HTTP ${status} must clear flow cookie and may set session cookie`)
    const flow = cookies.find(value => /^(tendo_oidc|__Host-tendo_oidc)=/.test(value))
    assert.ok(flow, `${path} HTTP ${status} missing flow cookie clear`)
    assertFlowCookie(path,status,flow,true)
    const sessionCookies = cookies.filter(value => /^(tendo_session|__Host-tendo_session)=/.test(value))
    assert.equal(cookies.length, 1 + sessionCookies.length, `${path} HTTP ${status} sends an undocumented cookie`)
    assert.equal(cookies.filter(value => /^(tendo_oidc|__Host-tendo_oidc)=/.test(value)).length, 1)
    if (sessionCookies[0]) assertSessionCookie(path, 201, sessionCookies[0])
  } else assert.equal(headers['set-cookie'], undefined, `${path} HTTP ${status} sends an undocumented Set-Cookie`)
  if (responseSpec.headers?.['Referrer-Policy']) assert.equal(headers['referrer-policy'], 'no-referrer')
  if (status === 303) {
    assert.equal(headers['referrer-policy'], 'no-referrer', `${path} HTTP 303 must set Referrer-Policy`)
    assert.ok(headers.location, `${path} HTTP 303 missing Location`)
  }
  if (responseSpec.headers?.Location) {
    const locationSchema = responseSpec.headers.Location.schema ?? {}
    if (locationSchema.const !== undefined) assert.equal(headers.location, locationSchema.const, `${path} HTTP ${status} has invalid Location`)
    else {
      assert.ok(headers.location, `${path} HTTP ${status} missing Location`)
      if (locationSchema.pattern) assert.match(headers.location, new RegExp(locationSchema.pattern), `${path} HTTP ${status} has invalid Location`)
    }
  }
  assertBody(path, status, body, method)
  if (path === '/api/v1/auth/oidc' && method === 'get' && status === 200) {
    assert.equal(typeof body.enabled, 'boolean', `${path} enabled must be boolean`)
    assert.equal(Object.hasOwn(body, 'displayName'), body.enabled, `${path} displayName must be present exactly when enabled`)
    if (body.enabled) assert.equal(typeof body.displayName, 'string', `${path} displayName must be a string`)
  }
  if (path === '/api/v1/auth/oidc/callback' && status === 303) {
    assert.equal(body, undefined, `${path} HTTP 303 must have an empty body`)
  }
  if (body && typeof body === 'object' && 'status' in body && expectedMedia.includes('application/problem+json')) {
    assert.equal(body.status, Number(status), `${path} HTTP ${status} problem status does not match HTTP status`)
  }
}

for (const [path, pathItem] of Object.entries(contract.paths)) {
  for (const [method, operation] of Object.entries(pathItem)) {
    if (!operation?.responses) continue
    for (const [status, response] of Object.entries(operation.responses)) {
      for (const [media, content] of Object.entries(response.content ?? {})) {
        if (content.example) assertBody(path, Number(status), content.example, method)
        const schemaName = content.schema?.$ref?.split('/').at(-1)
        assert.ok(schemaName && schemas[schemaName], `${path} ${method} ${status} ${media}: response schema missing`)
      }
    }
  }
}

const fixturePath = process.argv[2] ?? process.env.API_RESPONSE_FIXTURES
if (fixturePath) {
  const fixtures = JSON.parse(await readFile(fixturePath, 'utf8'))
  assert.ok(Array.isArray(fixtures), 'response fixtures must be a JSON array')
  for (const fixture of fixtures) {
    assertResponse(fixture.path, fixture.status, fixture.body, fixture.method.toLowerCase(), Object.fromEntries(Object.entries(fixture.headers ?? {}).map(([key, value]) => [key.toLowerCase(), value])))
  }
}

const setupOrigin = process.env.SETUP_URL ?? process.env.CONTRACT_SETUP_ORIGIN
if (setupOrigin) {
  const token = process.env.SETUP_TOKEN
  const setupRequest = {
    login: process.env.SETUP_LOGIN ?? 'contract-smoke-owner',
    password: process.env.SETUP_PASSWORD ?? 'contract-smoke-passphrase-unique',
    householdName: process.env.SETUP_HOUSEHOLD_NAME ?? 'Contract smoke household',
    timezone: process.env.SETUP_TIMEZONE ?? 'UTC',
  }
  const response = await fetch(new URL('/api/v1/auth/setup', setupOrigin), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Origin: setupOrigin, ...(token ? { 'X-Tendo-Setup-Token': token } : {}) },
    body: JSON.stringify(setupRequest),
    signal: AbortSignal.timeout(3000),
  })
  const headers = Object.fromEntries(response.headers.entries())
  const raw = await response.text()
  const body = raw ? JSON.parse(raw) : undefined
  assertResponse('/api/v1/auth/setup', response.status, body, 'put', headers)
}

const healthOrigin = process.env.RUNTIME_HEALTH_ORIGIN
if (healthOrigin) {
  for (const [path, expectedCodes] of [['/health/live', [200]], ['/health/ready', process.env.RUNTIME_HEALTH_EXPECT === 'unavailable' ? [503] : [200]]]) {
    const response = await fetch(new URL(path, healthOrigin), { headers: { 'X-Request-ID': 'contract-smoke_01' }, signal: AbortSignal.timeout(3000) })
    assert.ok(expectedCodes.includes(response.status), `${path}: unexpected HTTP ${response.status}`)
    const expectedMediaType = response.status === 503 ? 'application/problem+json' : 'application/json'
    assert.equal(response.headers.get('content-type')?.split(';')[0], expectedMediaType)
    const requestId = response.headers.get('x-request-id')
    assert.ok(/^[A-Za-z0-9._-]{1,64}$/.test(requestId ?? ''), `${path}: invalid request ID ${requestId}`)
    assert.equal(requestId, 'contract-smoke_01')
    assert.equal(response.headers.get('cache-control'), 'no-store')
    const body = await response.json()
    assertResponse(path, response.status, body, 'get', Object.fromEntries(response.headers.entries()))
    if (response.status === 503) assert.deepEqual(body, { type: 'about:blank', title: 'Service Unavailable', status: 503 })
    else assert.deepEqual(body, { status: 'ok' })
  }
}
