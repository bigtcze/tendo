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

function assertResponse(path, status, body, method, headers = {}) {
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
  if (responseSpec.headers?.ETag) assert.ok(typeof headers.etag === 'string' && headers.etag.length > 0, `${path} HTTP ${status} missing ETag`)
  if (responseSpec.headers?.Location) {
    const locationSchema = responseSpec.headers.Location.schema ?? {}
    if (locationSchema.const !== undefined) assert.equal(headers.location, locationSchema.const, `${path} HTTP ${status} has invalid Location`)
    else assert.ok(headers.location, `${path} HTTP ${status} missing Location`)
  }
  assertBody(path, status, body, method)
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
