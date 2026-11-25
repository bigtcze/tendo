import { expect, type Page } from '@playwright/test';
import { createHash, randomBytes } from 'node:crypto';
import { api, BASE_URL } from './fixtures';

type Scope = 'tendo:read' | 'tendo:write';

/**
 * Starts one real authorization request for a freshly registered client and opens its consent page. The page clock
 * must already be paused, so Allow stays waiting until the caller moves it on.
 */
export async function openConsent(page: Page, clientName: string, redirect: string) {
  const registered = await api(page, 'post', '/oauth/register', { redirect_uris: [redirect], client_name: clientName });
  expect(registered.status(), 'register the visual client').toBe(201);
  const clientId = (await registered.json() as { client_id: string }).client_id;
  const query = new URLSearchParams({
    response_type: 'code', client_id: clientId, redirect_uri: redirect,
    code_challenge: createHash('sha256').update(randomBytes(32).toString('base64url')).digest('base64url'),
    code_challenge_method: 'S256', state: randomBytes(12).toString('base64url'), resource: `${BASE_URL}/mcp`,
  });
  const authorized = page.waitForResponse((response) => new URL(response.url()).pathname === '/oauth/authorize');
  await page.goto(`/oauth/authorize?${query}`);
  expect((await authorized).status(), 'authorize redirects to the consent page').toBe(303);
}

/**
 * Replaces the signed-in account's own AI app settings through the API, naming the version it read. The settings
 * can only narrow, so this is how a visual run shows a consent with less allowed than asked, or nothing.
 */
export async function setOwnAiApps(page: Page, enabled: boolean, scopes: Scope[]) {
  const path = '/api/v1/mcp/settings';
  const current = await api(page, 'get', path);
  expect(current.status()).toBe(200);
  const saved = await page.request.put(path, {
    headers: { Origin: BASE_URL!, 'If-Match': current.headers().etag ?? '' }, data: { enabled, scopes },
  });
  expect(saved.status(), 'save own AI app settings').toBe(200);
}
