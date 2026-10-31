import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { parse } from 'yaml'

// package.json overrides minimatch to 11.x under @redocly/openapi-core 1.x
// (GHSA-g3hv-75wg-m249; openapi-core 1.x is archived and pins minimatch ^5).
// openapi-core 1.x calls `require('minimatch')(url, pattern)` only in
// readFileFromUrl when resolve.http.headers are configured; minimatch 11 is not
// callable as a module. Headers can only come from a Redocly config file (or
// Redocly login tokens, which are read only when such a file is loaded), and
// URLs are fetched only for remote $refs. Keep both absent while the override
// exists. See docs/development/dependencies.md.

const root = new URL('.', import.meta.url)
const require = createRequire(import.meta.url)

test('API tooling has no Redocly config that could configure HTTP resolve headers', async () => {
  const entries = await readdir(root)
  const configs = entries.filter(name => /^\.?redocly\.ya?ml$/.test(name))
  assert.deepEqual(configs, [], 'a Redocly config reaches openapi-core readFileFromUrl header matching; revisit the minimatch override first')
})

test('OpenAPI contract uses only local references', async () => {
  const contract = parse(await readFile(new URL('./openapi.yaml', root), 'utf8'))
  const external = []
  const walk = (value, path) => {
    if (Array.isArray(value)) return value.forEach((item, index) => walk(item, `${path}/${index}`))
    if (!value || typeof value !== 'object') return
    for (const [key, child] of Object.entries(value)) {
      if (key === '$ref' && (typeof child !== 'string' || !child.startsWith('#/'))) external.push(`${path}: ${child}`)
      walk(child, `${path}/${key}`)
    }
  }
  walk(contract, '#')
  assert.deepEqual(external, [], 'remote $refs reach openapi-core readFileFromUrl; revisit the minimatch override first')
})

test('openapi-core resolves the overridden minimatch and fetches without configured headers', async () => {
  const openapiCore = require.resolve('@redocly/openapi-core/lib/utils.js')
  const minimatch = createRequire(openapiCore)('minimatch/package.json')
  assert.ok(Number(minimatch.version.split('.')[0]) >= 11, `openapi-core resolves minimatch ${minimatch.version}`)
  const { readFileFromUrl } = require('@redocly/openapi-core/lib/utils.js')
  const originalFetch = globalThis.fetch
  globalThis.fetch = async () => new Response('openapi: 3.1.0', { headers: { 'content-type': 'application/yaml' } })
  try {
    const result = await readFileFromUrl('https://example.invalid/openapi.yaml', { headers: [] })
    assert.equal(result.body, 'openapi: 3.1.0')
  } finally {
    globalThis.fetch = originalFetch
  }
})
