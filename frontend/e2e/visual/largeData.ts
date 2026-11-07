import { expect, type Page } from '@playwright/test';
import { en } from '../../src/i18n/en';
import { capture } from './capture';
import { api, idempotencyKey, type Fixture } from './fixtures';

const ACTIVE_ITEMS = 1_000;
const MAX_DOM_ELEMENTS = 3_000; // 2x observed steady-state ~1,400; rows are cursor-paged and Home groups capped.
const MAX_INITIAL_API_REQUESTS = 8; // session + bounded summary/list reads; no per-item request is permitted.

async function createBatch(page: Page, fixture: Fixture) {
  const subjectId = fixture.subjects.find(subject => subject.name === 'Domácnost')?.id;
  expect(subjectId).toBeTruthy();
  const started = performance.now();
  // Sequential on purpose: item ids are uuidv7 and Home/All items tie-break on id, so concurrent creates reorder rows.
  for (let n = 1; n <= ACTIVE_ITEMS; n += 1) {
    const response = await api(page, 'post', `${fixture.apiRoot}/items`, {
      title: `Úkol ${String(n).padStart(4, '0')}`, subjectId, recurrence: null,
    }, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create large fixture item ${n}`).toBe(201);
  }
  console.info(`visual-large-seed count=${ACTIVE_ITEMS} durationMs=${Math.round(performance.now() - started)}`);
}

async function observeScreen(page: Page, path: string, row: string, width: number) {
  const requests: string[] = [];
  const listen = (request: import('@playwright/test').Request) => {
    if (new URL(request.url()).pathname.startsWith('/api/')) requests.push(`${request.method()} ${new URL(request.url()).pathname}`);
  };
  page.on('request', listen);
  const navStart = performance.now();
  await page.goto(path);
  await expect(page.locator(row).first()).toBeVisible({ timeout: 30_000 });
  const renderMs = performance.now() - navStart;
  // Bounds cover the settled screen: subject labels and members resolve after the first rows, so wait for them and
  // for the network to go quiet before counting DOM and requests; first-row latency stays a separate metric.
  await expect(page.locator(row).filter({ hasText: en['items.subjectLabels.loading'] })).toHaveCount(0);
  await page.waitForLoadState('networkidle');
  const domElements = await page.locator('*').count();
  await page.off('request', listen);
  expect(domElements, `${path} DOM bound ${MAX_DOM_ELEMENTS}`).toBeLessThan(MAX_DOM_ELEMENTS);
  expect(requests.length, `${path} initial API request bound ${MAX_INITIAL_API_REQUESTS}: ${requests.join(', ')}`).toBeLessThanOrEqual(MAX_INITIAL_API_REQUESTS);
  console.info(`visual-large-render route=${path} width=${width} apiRequests=${requests.length} domElements=${domElements} navigationToFirstRowMs=${Math.round(renderMs)} navigationToSettledMs=${Math.round(performance.now() - navStart)}`);
  return domElements;
}

export async function captureLargeDataset(page: Page, fixture: Fixture) {
  const initialSummary = await api(page, 'get', `${fixture.apiRoot}/items/summary`);
  expect(initialSummary.status()).toBe(200);
  const initialCounts = await initialSummary.json() as { needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number };
  const initialTotal = initialCounts.needsAttention + initialCounts.inProgress + initialCounts.waiting + initialCounts.upcoming + initialCounts.paused;
  await createBatch(page, fixture);
  const summary = await api(page, 'get', `${fixture.apiRoot}/items/summary`);
  expect(summary.status()).toBe(200);
  const counts = await summary.json() as { needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number };
  const activeTotal = counts.needsAttention + counts.inProgress + counts.waiting + counts.upcoming + counts.paused;
  expect(activeTotal - initialTotal).toBe(ACTIVE_ITEMS);
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await observeScreen(page, '/', 'main li[data-item-id]', width);
    await expect(page.getByRole('heading', { level: 2, name: /Needs attention/ })).toContainText(String(counts.needsAttention));
    await capture(page, `large-home-${width}`, width, 900, false);
    await observeScreen(page, '/items', 'main li[data-item-id]', width);
    await expect(page.locator('main li[data-item-id]')).toHaveCount(25);
    await capture(page, `large-all-items-${width}`, width, 900, false);
  }
}
