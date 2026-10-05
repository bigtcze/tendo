import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { parse } from 'yaml'

const contract = parse(await readFile(new URL('./openapi.yaml', import.meta.url), 'utf8'))
const generated = await readFile(new URL('./generated/health.d.ts', import.meta.url), 'utf8')

for (const [path, operation] of Object.entries(contract.paths)) {
  assert.ok(generated.includes(JSON.stringify(path)), `TypeScript output missing ${path}`)
  assert.ok(generated.includes(operation.get.operationId), `TypeScript output missing ${operation.get.operationId}`)
}
for (const name of Object.keys(contract.components.schemas)) {
  assert.ok(generated.includes(`${name}:`), `TypeScript output missing schema ${name}`)
}
for (const [key, schema] of Object.entries(contract.components.schemas.StatusResponse.properties)) {
  for (const value of schema.enum ?? []) assert.ok(generated.includes(JSON.stringify(value)), `TypeScript output missing ${key} value ${value}`)
}
for (const [path, operation] of Object.entries(contract.paths)) {
  for (const [status, response] of Object.entries(operation.get.responses)) {
    for (const [media, content] of Object.entries(response.content ?? {})) {
      assert.ok(generated.includes(`"${media}": components["schemas"]["${content.schema.$ref.split('/').at(-1)}"]`), `TypeScript output missing ${path} HTTP ${status} ${media}`)
    }
  }
}

const go = await readFile(new URL('../backend/internal/platform/httpx/health.gen.go', import.meta.url), 'utf8')
assert.match(go, /type StatusResponse struct/)
assert.match(go, /Status string `json:"status"`/)
for (const value of contract.components.schemas.StatusResponse.properties.status.enum) {
  assert.ok(generated.includes(JSON.stringify(value)), `TypeScript output missing status value ${value}`)
}
const generatedProblem = go.slice(go.indexOf('type Problem struct'), go.indexOf('// StatusResponse defines'))
for (const field of ['Detail', 'Instance', 'Status', 'Title', 'Type']) {
  assert.ok(generatedProblem.includes(`${field} `), `Go output missing Problem.${field}`)
}
