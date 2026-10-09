import test from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse } from 'yaml'

const root = new URL('.', import.meta.url)
const contract = parse(await readFile(new URL('./openapi.yaml', root), 'utf8'))
const checker = fileURLToPath(new URL('./check-health.mjs', root))
const requestId = 'middleware-contract_01'

test('OIDC foreign and malformed Origin middleware responses are documented and checked', async () => {
  const routes = [
    ['/api/v1/auth/oidc', 'get'],
    ['/api/v1/auth/oidc/identity', 'get'],
    ['/api/v1/auth/oidc/start', 'post'],
    ['/api/v1/auth/oidc/callback', 'get'],
  ]
  const dir = await mkdtemp(join(tmpdir(), 'tendo-oidc-origin-contract-'))
  try {
    for (const [path, method] of routes) {
      const response = contract.paths[path][method].responses['403']
      assert.ok(response, `${method.toUpperCase()} ${path} must document Origin rejection`)
      assert.equal(response.content['application/problem+json'].schema.$ref, '#/components/schemas/Problem')
      for (const fixture of [
        { origin: 'https://foreign.example' },
        { origin: ['https://tendo.test', 'https://tendo.test'] },
      ]) {
        // Pinned to the real middleware output by TestOIDCRoutesOriginRejectionMatchesContractFixture (backend/internal/platform/httpx).
        const headers = { 'content-type': 'application/problem+json', 'cache-control': 'no-store', 'x-request-id': requestId }
        const body = { type: 'about:blank', title: 'Forbidden', status: 403 }
        const fixtureFile = join(dir, 'response.json')
        await writeFile(fixtureFile, JSON.stringify([{ path, method, status: 403, headers, body, requestOrigin: fixture.origin }]))
        const child = spawn(process.execPath, [checker, fixtureFile], { env: { ...process.env, RUNTIME_HEALTH_ORIGIN: undefined, SETUP_URL: undefined }, stdio: ['ignore', 'ignore', 'pipe'] })
        let stderr = ''
        child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk })
        const [code] = await once(child, 'close')
        assert.equal(code, 0, `${method.toUpperCase()} ${path} middleware 403 was rejected: ${stderr}`)
      }
    }
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})
