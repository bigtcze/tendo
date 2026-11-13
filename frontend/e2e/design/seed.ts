import { expect, type Page } from '@playwright/test';
import { api, idempotencyKey } from '../visual/fixtures';

export const LARGE_ITEM_COUNT = 1_000;

const ACTIVE_GROUP_KEYS = ['needsAttention', 'inProgress', 'waiting', 'upcoming', 'paused'] as const;

/**
 * Adds a 1000+ active Home state for the two widths where a long list is the design question.
 *
 * Sequential on purpose: item ids are uuidv7 and Home tie-breaks rows that share an attention date on id, so
 * creating concurrently would reorder equal-date rows between runs and make a baseline noisy for no gain.
 */
export async function seedLargeDataset(page: Page, apiRoot: string, subjectId: string): Promise<number> {
  const started = Date.now();
  for (let n = 1; n <= LARGE_ITEM_COUNT; n += 1) {
    const attention = new Date('2026-05-14T00:00:00Z');
    attention.setUTCDate(attention.getUTCDate() + (n % 90) - 45);
    const response = await api(page, 'post', `${apiRoot}/items`, {
      title: `Úkol ${String(n).padStart(4, '0')}`,
      subjectId,
      attentionOn: attention.toISOString().slice(0, 10),
      recurrence: null,
    }, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create large design fixture item ${n}`).toBe(201);
  }
  const summary = await api(page, 'get', `${apiRoot}/items/summary`);
  expect(summary.status(), 'read large design fixture summary').toBe(200);
  const counts = await summary.json() as Record<string, number>;
  const active = ACTIVE_GROUP_KEYS.reduce((total, key) => total + (counts[key] ?? 0), 0);
  expect(active, 'large design fixture active item count').toBeGreaterThanOrEqual(LARGE_ITEM_COUNT);
  console.info(`design-capture-large-seed count=${LARGE_ITEM_COUNT} active=${active} durationMs=${Date.now() - started}`);
  return active;
}
