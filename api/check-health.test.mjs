import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import test from 'node:test'

const checker = new URL('./check-health.mjs', import.meta.url)
const requestId = 'contract-smoke_01'

async function checkResponse({ status, headers = {}, body, unavailable = false }) {
  const server = createServer((request, response) => {
    if (request.url === '/health/live') {
      response.writeHead(200, { ...healthyHeaders, 'x-request-id': requestId })
      response.end(JSON.stringify({ status: 'ok' }))
      return
    }
    response.writeHead(status, headers)
    response.end(body)
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const { port } = server.address()
  const childEnv = { ...process.env, RUNTIME_HEALTH_ORIGIN: `http://127.0.0.1:${port}` }
  if (unavailable) childEnv.RUNTIME_HEALTH_EXPECT = 'unavailable'
  else delete childEnv.RUNTIME_HEALTH_EXPECT
  const child = spawn(process.execPath, [checker.pathname], {
    env: childEnv,
    stdio: ['ignore', 'ignore', 'pipe'],
  })
  let stderr = ''
  child.stderr.setEncoding('utf8').on('data', (chunk) => { stderr += chunk })
  const timer = setTimeout(() => child.kill('SIGKILL'), 5000)
  const closed = once(child, 'close')
  try {
    const [code, signal] = await closed
    return { code, signal, stderr }
  } finally {
    clearTimeout(timer)
    server.close()
    await once(server, 'close')
  }
}

const healthyHeaders = {
  'content-type': 'application/json',
  'x-request-id': 'contract-smoke_01',
  'cache-control': 'no-store',
}
const unavailableHeaders = {
  'content-type': 'application/problem+json',
  'x-request-id': 'contract-smoke_01',
  'cache-control': 'no-store',
}

const cases = [
  ['valid readiness response passes', {
    status: 200, headers: healthyHeaders, body: JSON.stringify({ status: 'ok' }), succeeds: true,
  }],
  ['malformed readiness 200 text/plain fails', {
    status: 200, headers: { ...healthyHeaders, 'content-type': 'text/plain' }, body: 'ok',
  }],
  ['malformed readiness 200 JSON fails', {
    status: 200, headers: healthyHeaders, body: '{invalid',
  }],
  ['readiness 200 missing required headers fails', {
    status: 200, headers: { 'content-type': 'application/json' }, body: JSON.stringify({ status: 'ok' }),
  }],
  ['valid unavailable response passes', {
    status: 503, headers: unavailableHeaders,
    body: JSON.stringify({ type: 'about:blank', title: 'Service Unavailable', status: 503 }), unavailable: true, succeeds: true,
  }],
  ['malformed readiness 503 fails', {
    status: 503, headers: unavailableHeaders, body: '{invalid', unavailable: true,
  }],
  ['readiness 204 fails when a JSON response is required', {
    status: 204, headers: healthyHeaders, body: '',
  }],
]

test('operation coverage gate fails when any OpenAPI operation lacks a captured response', async () => {
  const { mkdtemp, writeFile, rm } = await import('node:fs/promises')
  const directory = await mkdtemp(join(tmpdir(), 'tendo-operation-coverage-'))
  const fixturePath = join(directory, 'responses.json')
  const fixture = { path: '/health/live', method: 'get', status: 200, headers: { 'x-request-id': requestId, 'cache-control': 'no-store', 'content-type': 'application/json' }, body: { status: 'ok' } }
  try {
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname, fixturePath], { env: { ...process.env, API_REQUIRE_OPERATION_COVERAGE: '1', API_OPERATION_COVERAGE_EXEMPTIONS: '', RUNTIME_HEALTH_ORIGIN: undefined, SETUP_URL: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''; child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    assert.notEqual(code, 0)
    assert.match(stderr, /OpenAPI operations have no captured HTTP response/)
    assert.match(stderr, /getInitialSetup/)
  } finally { await rm(directory, { recursive: true, force: true }) }
})

test('operation coverage gate fails when required mode has no captured responses', async () => {
  const env = { ...process.env, API_REQUIRE_OPERATION_COVERAGE: '1', API_OPERATION_COVERAGE_SCOPE: 'getInitialSetup' }
  for (const key of ['API_RESPONSE_FIXTURES', 'SETUP_URL', 'CONTRACT_SETUP_ORIGIN', 'RUNTIME_HEALTH_ORIGIN']) delete env[key]
  const child = spawn(process.execPath, [checker.pathname], { env, stdio: ['ignore', 'ignore', 'pipe'] })
  let stderr = ''; child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
  const [code] = await once(child, 'close')
  assert.notEqual(code, 0)
  assert.match(stderr, /requires captured responses/)
})

test('smoke coverage scopes partition every OpenAPI operationId', async () => {
  const { readFile } = await import('node:fs/promises')
  const { parse } = await import('yaml')
  const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
  const operationIds = Object.values(contract.paths)
    .flatMap(pathItem => Object.values(pathItem).map(operation => operation?.operationId).filter(Boolean))
  const scripts = new URL('../scripts/', import.meta.url)
  const read = name => readFile(new URL(name, scripts), 'utf8')
  const ids = text => text.match(/[A-Za-z]+/g) ?? []
  const setup = ids((await read('setup_smoke_api_capture.py')).match(/COVERAGE_SCOPE = ','\.join\(\(([\s\S]*?)\)\)/)[1])
  const oidc = ids((await read('oidc-smoke.sh')).match(/API_OPERATION_COVERAGE_SCOPE=([\s\S]*?) \\\n {2}node/)[1].replace(/\\\n/g, ''))
  const runtime = ids((await read('setup-smoke.sh')).match(/API_OPERATION_COVERAGE_SCOPE=(\S+)/)[1])
  const mcp = ids((await read('mcp-smoke.sh')).match(/API_OPERATION_COVERAGE_SCOPE=([\s\S]*?) \\\n {2}node/)[1].replace(/\\\n/g, ''))
  const scoped = [...setup, ...oidc, ...runtime, ...mcp]
  assert.deepEqual([...scoped].sort(), [...new Set(scoped)].sort(), 'an operationId appears in more than one smoke scope')
  assert.deepEqual([...scoped].sort(), [...operationIds].sort(), 'smoke coverage scopes must cover every OpenAPI operationId exactly once')
})

test('health checker accepts the backend OIDC start Set-Cookie format and rejects malformed cookies', async () => {
  const directory = await (await import('node:fs/promises')).mkdtemp(join(tmpdir(), 'tendo-oidc-start-cookie-'))
  const fixturePath = join(directory, 'responses.json')
  const run = async (cookie) => {
    const fixture = { path: '/api/v1/auth/oidc/start', method: 'post', status: 200, headers: { 'x-request-id': requestId, 'cache-control': 'no-store', 'content-type': 'application/json', 'set-cookie': cookie }, body: { authorizationUrl: 'https://issuer.test/authorize' } }
    await (await import('node:fs/promises')).writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath,[checker.pathname,fixturePath],{env:{...process.env,RUNTIME_HEALTH_ORIGIN:undefined,SETUP_URL:undefined},stdio:['ignore','ignore','pipe']})
    let stderr='';child.stderr.setEncoding('utf8').on('data',chunk=>{stderr+=chunk});const [code]=await once(child,'close');return {code,stderr}
  }
  try {
    const actual='tendo_oidc=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/; Max-Age=600; HttpOnly; SameSite=Lax'
    const accepted=await run(actual);assert.equal(accepted.code,0,accepted.stderr)
    const secureHost=await run('__Host-tendo_oidc=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/; Max-Age=600; HttpOnly; SameSite=Lax; Secure');assert.equal(secureHost.code,0,secureHost.stderr)
    for(const malformed of [undefined,'tendo_oidc=bad; Path=/; Max-Age=600; HttpOnly; SameSite=Lax','tendo_oidc=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/; Max-Age=600; SameSite=Lax','tendo_oidc=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/; Max-Age=600; HttpOnly; SameSite=Lax; Domain=evil.test','__Host-tendo_oidc=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; Path=/; Max-Age=600; HttpOnly; SameSite=Lax']) { const rejected=await run(malformed);assert.notEqual(rejected.code,0,`accepted malformed flow cookie: ${malformed}`) }
  } finally { await (await import('node:fs/promises')).rm(directory,{recursive:true,force:true}) }
})

test('health checker validates OIDC 303 headers and multiple Set-Cookie fields', async () => {
  const directory = await (await import('node:fs/promises')).mkdtemp(join(tmpdir(), 'tendo-oidc-contract-'))
  const fixturePath = join(directory, 'responses.json')
  const run = async (headers) => {
    const fixture = { path: '/api/v1/auth/oidc/callback', method: 'get', status: 303, headers: { 'x-request-id': requestId, 'cache-control': 'no-store', 'referrer-policy': 'no-referrer', location: '/', ...headers }, body: undefined }
    await (await import('node:fs/promises')).writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname, fixturePath], { env: { ...process.env, RUNTIME_HEALTH_ORIGIN: undefined, SETUP_URL: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  }
  try {
    const good = await run({ 'set-cookie': ['tendo_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax', `tendo_session=${'A'.repeat(43)}; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax`] })
    assert.equal(good.code, 0, good.stderr)
    const unknown = await run({ 'set-cookie': ['tendo_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax', 'unexpected=secret'] })
    assert.notEqual(unknown.code, 0, 'checker must reject undocumented second cookie')
    const bad = await run({ 'set-cookie': ['tendo_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax'] , 'referrer-policy': 'unsafe-url' })
    assert.notEqual(bad.code, 0)
    const wrongBody = await run({ 'set-cookie': ['tendo_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax'] })
    assert.equal(wrongBody.code, 0, wrongBody.stderr)
  } finally { await (await import('node:fs/promises')).rm(directory, { recursive: true, force: true }) }
})

// Runs the checker on one captured fixture and reports its exit code and stderr.
async function checkFixture(fixture) {
  const { mkdtemp, writeFile, rm } = await import('node:fs/promises')
  const directory = await mkdtemp(join(tmpdir(), 'tendo-fixture-'))
  try {
    const fixturePath = join(directory, 'responses.json')
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname, fixturePath], { env: { ...process.env, RUNTIME_HEALTH_ORIGIN: undefined, SETUP_URL: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  } finally { await rm(directory, { recursive: true, force: true }) }
}

const consentHandle = 'H'.repeat(42) + '_'
const consentPath = `/api/v1/mcp/authorization-requests/${consentHandle}`
const consentSet = (name = 'tendo_mcp_consent', path = consentPath, extra = '') =>
  `${name}=${'B'.repeat(43)}; Path=${path}; Max-Age=300; HttpOnly; SameSite=Lax${extra}`
const consentClear = (name = 'tendo_mcp_consent', path = consentPath, extra = '') =>
  `${name}=; Path=${path}; Max-Age=0; HttpOnly; SameSite=Lax${extra}`

test('health checker validates the MCP authorize 303 consent binding cookie', async () => {
  const run = (location, cookie) => checkFixture({
    path: '/oauth/authorize', method: 'get', status: 303,
    headers: { 'x-request-id': requestId, 'cache-control': 'no-store', 'referrer-policy': 'no-referrer', location, ...(cookie === undefined ? {} : { 'set-cookie': cookie }) },
  })
  const success = `/connect#request=${consentHandle}`
  for (const [label, location, cookie] of [
    ['plain binding', success, consentSet()],
    ['secure binding', success, consentSet('__Secure-tendo_mcp_consent', consentPath, '; Secure')],
    ['refusal without a cookie', '/connect#error=invalid_request', undefined],
  ]) {
    const accepted = await run(location, cookie)
    assert.equal(accepted.code, 0, `${label}: ${accepted.stderr}`)
  }
  for (const [label, location, cookie] of [
    ['success without a cookie', success, undefined],
    ['refusal with a cookie', '/connect#error=invalid_request', consentSet()],
    ['session cookie name', success, consentSet('tendo_session')],
    ['secure name without Secure', success, consentSet('__Secure-tendo_mcp_consent')],
    ['Path of another handle', success, consentSet('tendo_mcp_consent', `/api/v1/mcp/authorization-requests/${'Z'.repeat(43)}`)],
    ['Path=/', success, consentSet('tendo_mcp_consent', '/')],
    ['short binding value', success, `tendo_mcp_consent=short; Path=${consentPath}; Max-Age=300; HttpOnly; SameSite=Lax`],
    ['wrong Max-Age', success, consentSet().replace('Max-Age=300', 'Max-Age=600')],
    ['missing HttpOnly', success, consentSet().replace('; HttpOnly', '')],
    ['SameSite=None', success, consentSet().replace('SameSite=Lax', 'SameSite=None')],
    ['Domain attribute', success, consentSet('tendo_mcp_consent', consentPath, '; Domain=evil.test')],
    ['two cookies', success, [consentSet(), consentSet()]],
    ['plain name with Secure', success, consentSet('tendo_mcp_consent', consentPath, '; Secure')],
    ['two Max-Age', success, consentSet() + '; Max-Age=300'],
    ['a clear instead of a set', success, consentClear()],
  ]) {
    const rejected = await run(location, cookie)
    assert.notEqual(rejected.code, 0, `checker accepted ${label}`)
  }
})

test('health checker validates the MCP decision consent binding clear', async () => {
  const decisionPath = '/api/v1/mcp/authorization-requests/{requestHandle}/decision'
  const run = (status, cookie, url) => checkFixture({
    path: decisionPath, ...(url === null ? {} : { url: url ?? `${consentPath}/decision` }), method: 'post', status,
    headers: { 'x-request-id': requestId, 'cache-control': 'no-store', 'content-type': status === 200 ? 'application/json' : 'application/problem+json', ...(cookie === undefined ? {} : { 'set-cookie': cookie }) },
    body: status === 200 ? { redirectTo: 'http://127.0.0.1:9/cb?code=x&iss=http%3A%2F%2Flocalhost' } : { type: 'about:blank', title: 'Not Found', status: 404, code: 'authorization_request_not_found' },
  })
  for (const status of [200, 404]) {
    const plain = await run(status, consentClear())
    assert.equal(plain.code, 0, `${status} plain clear: ${plain.stderr}`)
    const secure = await run(status, consentClear('__Secure-tendo_mcp_consent', consentPath, '; Secure'))
    assert.equal(secure.code, 0, `${status} secure clear: ${secure.stderr}`)
    if (status === 404) {
      // A malformed handle has no binding Path Tendo ever issued, so the 404 clears nothing.
      const malformedURL = '/api/v1/mcp/authorization-requests/short/decision'
      const malformed = await run(status, undefined, malformedURL)
      assert.equal(malformed.code, 0, `malformed handle without a clear: ${malformed.stderr}`)
      const cleared = await run(status, consentClear(), malformedURL)
      assert.notEqual(cleared.code, 0, 'checker accepted a clear for a malformed handle')
    }
    for (const [label, cookie, url] of [
      ['no clear', undefined],
      ['a set instead of a clear', consentSet()],
      ['session cookie clear', 'tendo_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax'],
      ['clear of another handle', consentClear('tendo_mcp_consent', `/api/v1/mcp/authorization-requests/${'Z'.repeat(43)}`)],
      ['clear without the request URL', consentClear(), null],
      ['no clear and no request URL', undefined, null],
      ['clear with a value', consentClear().replace('tendo_mcp_consent=;', `tendo_mcp_consent=${'B'.repeat(43)};`)],
      ['clear that does not expire', consentClear().replace('Max-Age=0', 'Max-Age=300')],
      ['url that is not the decision', consentClear(), consentPath],
      ['absolute url', consentClear(), `http://localhost${consentPath}/decision`],
      ['url with a query', consentClear(), `${consentPath}/decision?x=1`],
    ]) {
      const rejected = await run(status, cookie, url)
      assert.notEqual(rejected.code, 0, `${status}: checker accepted ${label}`)
    }
  }
})

test('health checker validates readiness response contract', async (t) => {
  for (const [name, response] of cases) {
    await t.test(name, async () => {
      const env = response.unavailable
        ? { ...process.env, RUNTIME_HEALTH_ORIGIN: undefined, RUNTIME_HEALTH_EXPECT: 'unavailable' }
        : process.env
      const result = await checkResponse({ ...response, env })
      assert.equal(result.code, response.succeeds ? 0 : 1, result.stderr)
      assert.equal(result.signal, null)
    })
  }
})
