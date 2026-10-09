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
    const bad = await run({ 'set-cookie': ['tendo_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax'] , 'referrer-policy': 'unsafe-url' })
    assert.notEqual(bad.code, 0)
  } finally { await (await import('node:fs/promises')).rm(directory, { recursive: true, force: true }) }
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
