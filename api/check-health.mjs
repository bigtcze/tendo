import assert from 'node:assert/strict'
import Ajv2020 from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'
import { parse } from 'yaml'
import { readFile } from 'node:fs/promises'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const ajv = new Ajv2020({ strict: false })
addFormats(ajv)
const schemas = Object.fromEntries(Object.entries(contract.components.schemas).map(([name, schema]) => [name, ajv.compile(schema)]))

function assertResponse(path, status, body) {
  const responseSpec = contract.paths[path].get.responses[String(status)]
  assert.ok(responseSpec, `${path}: undocumented HTTP ${status}`)
  const media = responseSpec.content?.['application/json'] ? 'application/json' : 'application/problem+json'
  const schemaRef = responseSpec.content?.[media]?.schema?.$ref
  const schemaName = schemaRef?.split('/').at(-1)
  assert.ok(schemaName && schemas[schemaName], `${path}: response schema missing`)
  assert.ok(schemas[schemaName](body), `${path} HTTP ${status} body invalid: ${JSON.stringify(schemas[schemaName].errors)}`)
}

for (const [path, method] of Object.entries(contract.paths)) {
  for (const [status, response] of Object.entries(method.get.responses)) {
    for (const [media, content] of Object.entries(response.content ?? {})) {
      if (content.example) assertResponse(path, Number(status), content.example)
    }
  }
}

const origin = process.env.RUNTIME_HEALTH_ORIGIN
if (origin) {
  for (const [path, expectedCodes] of [['/health/live', [200]], ['/health/ready', process.env.RUNTIME_HEALTH_EXPECT === 'unavailable' ? [503] : [200]]]) {
    const response = await fetch(new URL(path, origin), { headers: { 'X-Request-ID': 'contract-smoke_01' }, signal: AbortSignal.timeout(3000) })
    assert.ok(expectedCodes.includes(response.status), `${path}: unexpected HTTP ${response.status}`)
    const expectedMediaType = response.status === 503 ? 'application/problem+json' : 'application/json'
    assert.equal(response.headers.get('content-type')?.split(';')[0], expectedMediaType)
    const requestId = response.headers.get('x-request-id')
    assert.ok(/^[A-Za-z0-9._-]{1,64}$/.test(requestId ?? ''), `${path}: invalid request ID ${requestId}`)
    assert.equal(requestId, 'contract-smoke_01')
    assert.equal(response.headers.get('cache-control'), 'no-store')
    const body = await response.json()
    assertResponse(path, response.status, body)
    if (response.status === 503) {
      assert.deepEqual(body, { type: 'about:blank', title: 'Service Unavailable', status: 503 })
    } else {
      assert.deepEqual(body, { status: 'ok' })
    }
  }
}
