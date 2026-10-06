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

test('session cookie fixtures enforce cookie attributes, ETag pattern, and Location', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-api-session-'))
  const fixturePath = join(directory, 'responses.json')
  const token = 'A'.repeat(43)
  const session = { userId: '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60', login: 'owner', defaultHouseholdId: '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61', expiresAt: '2026-11-05T10:00:00Z' }
  const base = { 'content-type': 'application/json', 'x-request-id': 'contract-check', 'cache-control': 'no-store' }
  const created = { path: '/api/v1/session', method: 'post', status: 201, headers: { ...base, location: '/api/v1/session', 'set-cookie': `tendo_session=${token}; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax` }, body: session }
  const current = { path: '/api/v1/session', method: 'get', status: 200, headers: { ...base, etag: '"session-0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"' }, body: session }
  const deleted = { path: '/api/v1/session', method: 'delete', status: 204, headers: { 'x-request-id': 'contract-check', 'cache-control': 'no-store', 'set-cookie': 'tendo_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax' } }
  const unauthenticated = { path: '/api/v1/session', method: 'get', status: 401, headers: { ...base, 'content-type': 'application/problem+json' }, body: { type: 'about:blank', title: 'Unauthorized', status: 401, code: 'unauthenticated' } }
  const run = async fixture => {
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname], { env: { ...process.env, API_RESPONSE_FIXTURES: fixturePath, SETUP_URL: undefined, CONTRACT_SETUP_ORIGIN: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  }
  try {
    const staleCleared = { ...unauthenticated, headers: { ...unauthenticated.headers, 'set-cookie': 'tendo_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax' } }
    for (const fixture of [created, current, deleted, unauthenticated, staleCleared]) assert.equal((await run(fixture)).code, 0, JSON.stringify(fixture))
    for (const [name, fixture, mutate, diagnostic] of [
      ['missing HttpOnly', created, f => { f.headers['set-cookie'] = f.headers['set-cookie'].replace('; HttpOnly', '') }, 'HttpOnly'],
      ['wrong SameSite', created, f => { f.headers['set-cookie'] = f.headers['set-cookie'].replace('Lax', 'None') }, 'SameSite=Lax'],
      ['Domain attribute', created, f => { f.headers['set-cookie'] += '; Domain=example.com' }, 'Domain'],
      ['__Host- without Secure', created, f => { f.headers['set-cookie'] = f.headers['set-cookie'].replace('tendo_session=', '__Host-tendo_session=') }, 'Secure'],
      ['short token', created, f => { f.headers['set-cookie'] = f.headers['set-cookie'].replace(token, 'short') }, '43 base64url'],
      ['missing cookie', created, f => { delete f.headers['set-cookie'] }, 'missing Set-Cookie'],
      ['non-clearing 401', staleCleared, f => { f.headers['set-cookie'] = `tendo_session=${token}; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax` }, 'clearing cookie must be empty'],
      ['undocumented cookie on 200', current, f => { f.headers['set-cookie'] = 'tendo_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax' }, 'undocumented Set-Cookie'],
      ['non-clearing delete', deleted, f => { f.headers['set-cookie'] = f.headers['set-cookie'].replace('Max-Age=0', 'Max-Age=5') }, 'Max-Age=0'],
      ['bad ETag', current, f => { f.headers.etag = '"other"' }, 'ETag does not match'],
      ['wrong Location', created, f => { f.headers.location = '/api/v1/other' }, 'invalid Location'],
      ['token in body', created, f => { f.body = { ...session, token } }, 'body invalid'],
    ]) {
      await t.test(name, async () => {
        const copy = structuredClone(fixture)
        mutate(copy)
        const result = await run(copy)
        assert.notEqual(result.code, 0)
        assert.match(result.stderr, new RegExp(diagnostic))
      })
    }
  } finally { await rm(directory, { recursive: true, force: true }) }
})

test('household fixtures enforce the ETag pattern, problem media type, and body schema', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-api-household-'))
  const fixturePath = join(directory, 'responses.json')
  const path = '/api/v1/households/{householdId}'
  const base = { 'x-request-id': 'contract-check', 'cache-control': 'no-store' }
  const household = { id: '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61', name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' }
  const ok = { path, method: 'get', status: 200, headers: { ...base, 'content-type': 'application/json', etag: '"1"' }, body: household }
  const notFound = { path, method: 'get', status: 404, headers: { ...base, 'content-type': 'application/problem+json' }, body: { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found' } }
  const run = async fixture => {
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname], { env: { ...process.env, API_RESPONSE_FIXTURES: fixturePath, SETUP_URL: undefined, CONTRACT_SETUP_ORIGIN: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  }
  try {
    for (const fixture of [ok, notFound]) assert.equal((await run(fixture)).code, 0, JSON.stringify(fixture))
    for (const [name, fixture, mutate, diagnostic] of [
      ['unquoted ETag', ok, f => { f.headers.etag = '1' }, 'ETag does not match'],
      ['weak ETag', ok, f => { f.headers.etag = 'W/"1"' }, 'ETag does not match'],
      ['zero version ETag', ok, f => { f.headers.etag = '"0"' }, 'ETag does not match'],
      ['missing ETag', ok, f => { delete f.headers.etag }, 'missing ETag'],
      ['extra version field', ok, f => { f.body = { ...household, version: 1 } }, 'body invalid'],
      ['missing timezone', ok, f => { f.body = { id: household.id, name: household.name, createdAt: household.createdAt } }, 'body invalid'],
      ['404 wrong media', notFound, f => { f.headers['content-type'] = 'application/json' }, 'wrong media type'],
      ['404 status mismatch', notFound, f => { f.body.status = 403 }, 'problem status does not match HTTP status'],
      ['undocumented status', notFound, f => { f.status = 409; f.body.status = 409 }, 'undocumented HTTP 409'],
    ]) {
      await t.test(name, async () => {
        const copy = structuredClone(fixture)
        mutate(copy)
        const result = await run(copy)
        assert.notEqual(result.code, 0)
        assert.match(result.stderr, new RegExp(diagnostic))
      })
    }
  } finally { await rm(directory, { recursive: true, force: true }) }
})

test('subject fixtures enforce Location, ETag, list envelope, concurrency problems, and strict bodies', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-api-subject-'))
  const fixturePath = join(directory, 'responses.json')
  const household = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'
  const id = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70'
  const collection = '/api/v1/households/{householdId}/subjects'
  const item = '/api/v1/households/{householdId}/subjects/{subjectId}'
  const base = { 'x-request-id': 'contract-check', 'cache-control': 'no-store' }
  const json = { ...base, 'content-type': 'application/json' }
  const problem = { ...base, 'content-type': 'application/problem+json' }
  const subject = { id, type: 'vehicle', name: 'Octavia', archived: false, createdAt: '2026-10-07T10:00:00Z', updatedAt: '2026-10-07T10:00:00Z' }
  const created = { path: collection, method: 'post', status: 201, headers: { ...json, etag: '"1"', location: `/api/v1/households/${household}/subjects/${id}` }, body: subject }
  const list = { path: collection, method: 'get', status: 200, headers: json, body: { items: [subject], nextCursor: null } }
  const listNext = { ...structuredClone(list), body: { items: [subject], nextCursor: 'czE6MDE5OGEyZjA' } }
  const got = { path: item, method: 'get', status: 200, headers: { ...json, etag: '"1"' }, body: subject }
  const patched = { path: item, method: 'patch', status: 200, headers: { ...json, etag: '"2"' }, body: { ...subject, archived: true } }
  const failed = { path: item, method: 'patch', status: 412, headers: problem, body: { type: 'about:blank', title: 'Precondition Failed', status: 412, code: 'precondition_failed' } }
  const required = { path: item, method: 'patch', status: 428, headers: problem, body: { type: 'about:blank', title: 'Precondition Required', status: 428, code: 'precondition_required' } }
  const badQuery = { path: collection, method: 'get', status: 400, headers: problem, body: { type: 'about:blank', title: 'Bad Request', status: 400, code: 'invalid_query', parameter: 'limit' } }
  const invalid = { path: collection, method: 'post', status: 422, headers: problem, body: { type: 'about:blank', title: 'Validation Failed', status: 422, code: 'invalid_length', field: 'name' } }
  const run = async fixture => {
    await writeFile(fixturePath, JSON.stringify([fixture]))
    const child = spawn(process.execPath, [checker.pathname], { env: { ...process.env, API_RESPONSE_FIXTURES: fixturePath, SETUP_URL: undefined, CONTRACT_SETUP_ORIGIN: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
    const [code] = await once(child, 'close')
    return { code, stderr }
  }
  try {
    for (const fixture of [created, list, listNext, got, patched, failed, required, badQuery, invalid]) assert.equal((await run(fixture)).code, 0, JSON.stringify(fixture))
    for (const [name, fixture, mutate, diagnostic] of [
      ['created without ETag', created, f => { delete f.headers.etag }, 'missing ETag'],
      ['created without Location', created, f => { delete f.headers.location }, 'missing Location'],
      ['created with wrong Location', created, f => { f.headers.location = '/api/v1/subjects/1' }, 'invalid Location'],
      ['weak ETag', got, f => { f.headers.etag = 'W/"1"' }, 'ETag does not match'],
      ['zero version ETag', patched, f => { f.headers.etag = '"0"' }, 'ETag does not match'],
      ['patched without ETag', patched, f => { delete f.headers.etag }, 'missing ETag'],
      ['list missing nextCursor', list, f => { delete f.body.nextCursor }, 'body invalid'],
      ['list nextCursor wrong type', list, f => { f.body.nextCursor = 5 }, 'body invalid'],
      ['list item extra field', list, f => { f.body.items[0].householdId = household }, 'body invalid'],
      ['subject leaks version', got, f => { f.body.version = 1 }, 'body invalid'],
      ['subject extra field', created, f => { f.body.extra = true }, 'body invalid'],
      ['subject unknown type', got, f => { f.body.type = 'robot' }, 'body invalid'],
      ['subject missing archived', got, f => { delete f.body.archived }, 'body invalid'],
      ['412 status mismatch', failed, f => { f.body.status = 428 }, 'problem status does not match HTTP status'],
      ['412 wrong media', failed, f => { f.headers['content-type'] = 'application/json' }, 'wrong media type'],
      ['428 documented on get is undocumented', { ...required, method: 'get', path: item }, () => {}, 'undocumented HTTP 428'],
      ['query problem unknown parameter', badQuery, f => { f.body.parameter = 'offset' }, 'body invalid'],
      ['422 unknown field', invalid, f => { f.body.field = 'archived' }, 'body invalid'],
      ['undocumented status on list', list, f => { f.status = 412 }, 'undocumented HTTP 412'],
    ]) {
      await t.test(name, async () => {
        const copy = structuredClone(fixture)
        mutate(copy)
        const result = await run(copy)
        assert.notEqual(result.code, 0)
        assert.match(result.stderr, new RegExp(diagnostic))
      })
    }
  } finally { await rm(directory, { recursive: true, force: true }) }
})
