import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

const checker = new URL('./check-health.mjs', import.meta.url)

async function checkSetupResponse({ status = 201, headers = {}, body = '{"required":false}' } = {}) {
  const server = createServer((request, response) => {
    request.resume()
    response.writeHead(status, headers)
    response.end(body)
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const { port } = server.address()
  const child = spawn(process.execPath, [checker.pathname], {
    env: { ...process.env, SETUP_URL: `http://127.0.0.1:${port}`, SETUP_TOKEN: undefined },
    stdio: ['ignore', 'ignore', 'pipe'],
  })
  let stderr = ''
  child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
  try {
    const [code] = await once(child, 'close')
    return { code, stderr }
  } finally {
    server.close()
    await once(server, 'close')
  }
}

test('setup live validator accepts its documented creation response', async () => {
  const result = await checkSetupResponse({ status: 201, headers: { 'content-type': 'application/json', 'x-request-id': 'contract-check', 'cache-control': 'no-store', location: '/api/v1/auth/setup' }, body: '{"required":false}' })
  assert.equal(result.code, 0, result.stderr)
})

test('captured response fixture validation enforces the same strict envelope', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-api-fixture-'))
  const fixturePath = join(directory, 'responses.json')
  const valid = { path: '/api/v1/auth/setup', method: 'put', status: 201, headers: { 'content-type': 'application/json', 'x-request-id': 'contract-check', 'cache-control': 'no-store', location: '/api/v1/auth/setup' }, body: { required: false } }
  const run = async fixture => {
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname], { env: { ...process.env, API_RESPONSE_FIXTURES: fixturePath, SETUP_URL: undefined, CONTRACT_SETUP_ORIGIN: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  }
  try {
    assert.equal((await run(valid)).code, 0)
    for (const [name, mutate, diagnostic] of [
      ['wrong media', fixture => { fixture.headers['content-type'] = 'text/plain' }, 'wrong media type'],
      ['missing ETag', fixture => { fixture.path = '/api/v1/auth/setup'; fixture.method = 'get'; fixture.status = 200; fixture.body = { required: true }; delete fixture.headers.location; fixture.headers.etag = undefined }, 'missing ETag'],
      ['problem status mismatch', fixture => { fixture.method = 'put'; fixture.status = 401; fixture.body = { type: 'about:blank', title: 'Unauthorized', status: 403 }; delete fixture.headers.location; fixture.headers['content-type'] = 'application/problem+json' }, 'problem status does not match HTTP status'],
    ]) {
      await t.test(name, async () => {
        const fixture = structuredClone(valid)
        mutate(fixture)
        const result = await run(fixture)
        assert.notEqual(result.code, 0)
        assert.match(result.stderr, new RegExp(diagnostic))
      })
    }
  } finally { await rm(directory, { recursive: true, force: true }) }
})

test('setup live validator enforces media, cache, request id, Location, and body schema', async t => {
  const validHeaders = {
    'content-type': 'application/json',
    'x-request-id': 'contract-smoke_01',
    'cache-control': 'no-store',
    location: '/api/v1/auth/setup',
  }
  for (const [name, override, expected] of [
    ['valid setup creation', {}, 0],
    ['wrong content type', { headers: { 'content-type': 'text/plain' } }, 1],
    ['missing cache header', { headers: { 'cache-control': undefined } }, 1],
    ['invalid request ID', { headers: { 'x-request-id': 'bad id' } }, 1],
    ['wrong Location', { headers: { location: '/wrong' } }, 1],
    ['invalid response body', { body: '{"required":"false"}' }, 1],
  ]) {
    await t.test(name, async () => {
      const headers = { ...validHeaders, ...override.headers }
      for (const [key, value] of Object.entries(headers)) if (value === undefined) delete headers[key]
      const result = await checkSetupResponse({ headers, body: override.body ?? '{"required":false}' })
      assert.equal(result.code, expected, result.stderr)
      if (expected !== 0) {
        const diagnostic = name === 'wrong content type' ? 'wrong media type'
          : name === 'missing cache header' ? 'cache-control must be no-store'
            : name === 'invalid request ID' ? 'invalid request ID'
              : name === 'wrong Location' ? 'invalid Location' : 'body invalid'
        assert.match(result.stderr, new RegExp(diagnostic))
      }
    })
  }
})
