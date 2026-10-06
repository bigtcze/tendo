import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { parse } from 'yaml'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const generated = await readFile(new URL('./generated/api.d.ts', import.meta.url), 'utf8')
const generatedHealth = await readFile(new URL('./generated/health.d.ts', import.meta.url), 'utf8')
const goSetup = await readFile(new URL('../backend/internal/identity/httpapi/setup.gen.go', import.meta.url), 'utf8')
const goHealth = await readFile(new URL('../backend/internal/platform/httpx/health.gen.go', import.meta.url), 'utf8')

for (const [path, pathItem] of Object.entries(contract.paths)) {
  assert.ok(generated.includes(JSON.stringify(path)), `TypeScript output missing ${path}`)
  for (const [method, operation] of Object.entries(pathItem)) {
    if (!operation || typeof operation !== 'object' || !operation.operationId) continue
    for (const output of [generated, generatedHealth]) {
      assert.ok(output.includes(operation.operationId), `TypeScript output missing ${operation.operationId}`)
    }
    for (const [status, response] of Object.entries(operation.responses ?? {})) {
      const responseSchema = response.content?.['application/json']?.schema ?? response.content?.['application/problem+json']?.schema
      if (responseSchema?.$ref) {
        const name = responseSchema.$ref.split('/').at(-1)
        const operationBlock = generated.slice(generated.indexOf(`${operation.operationId}:`), generated.indexOf('\n    };', generated.indexOf(`${operation.operationId}:`)))
        assert.ok(operationBlock.includes(`            ${status}:`) || operationBlock.includes(`            "${status}":`), `${path} ${method} TS response missing HTTP ${status}`)
        assert.ok(generated.includes(`components["schemas"]["${name}"]`), `${path} ${method} TS response missing ${name}`)
      }
      for (const [header, spec] of Object.entries(response.headers ?? {})) {
        if (spec.$ref) assert.ok(contract.components.headers[spec.$ref.split('/').at(-1)], `${path} ${status} has unresolved header ref`)
      }
    }
    if (operation.requestBody) {
      const schema = operation.requestBody.content?.['application/json']?.schema
      assert.ok(schema?.$ref && generated.includes(`components["schemas"]["${schema.$ref.split('/').at(-1)}"]`), `${operation.operationId}: missing generated request model`)
      assert.ok(operation.requestBody.content['application/json'].example, `${operation.operationId}: missing request example`)
    }
  }
}
for (const [name, schema] of Object.entries(contract.components.schemas)) {
  assert.ok(generated.includes(`${name}:`), `TypeScript output missing schema ${name}`)
  for (const field of Object.keys(schema.properties ?? {})) {
    assert.ok(generated.includes(field), `TypeScript output missing field ${name}.${field}`)
  }
}
for (const [name, schema] of Object.entries(contract.components.schemas)) {
  for (const value of schema.properties?.status?.enum ?? []) assert.ok(generated.includes(JSON.stringify(value)), `TypeScript output missing status value ${value}`)
}
for (const [name, schema] of Object.entries(contract.components.schemas)) {
  assert.ok(goSetup.includes(name) || goHealth.includes(name), `Generated Go missing model ${name}`)
}
for (const name of ['SetupRequest', 'SetupStatus', 'Problem', 'ValidationProblem', 'LoginRequest', 'Session']) assert.ok(goSetup.includes(`type ${name} struct`), `Generated setup Go missing ${name}`)
assert.ok(/type Session struct \{[^}]*DefaultHouseholdId \*string/s.test(goSetup), 'Generated Go Session must have optional DefaultHouseholdId')
assert.ok(goSetup.includes('SessionCookieScopes'), 'Generated Go missing SessionCookie security scheme')
for (const operationId of ['createSession', 'getSession', 'deleteSession']) assert.ok(generated.includes(`${operationId}:`), `TypeScript output missing operation ${operationId}`)
assert.ok(/Session: \{[^}]*defaultHouseholdId\?: string/s.test(generated), 'TypeScript Session must have optional defaultHouseholdId')
assert.ok(/LoginRequest: \{[^}]*login: string[^}]*password: string/s.test(generated), 'TypeScript LoginRequest missing required fields')
for (const name of ['StatusResponse', 'Problem']) assert.ok(goHealth.includes(`type ${name} struct`), `Generated health Go missing ${name}`)
