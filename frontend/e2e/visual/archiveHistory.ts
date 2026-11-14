import { expect, type Page } from '@playwright/test';
import { cs } from '../../src/i18n/cs';
import { en } from '../../src/i18n/en';
import { api, idempotencyKey, type Fixture, type VisualItem } from './fixtures';

type Locale = 'en' | 'cs';
const catalogFor = (locale: Locale) => (locale === 'cs' ? cs : en);
const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** One history page holds 20 receipts (UX.md "History"); 25 gives a full first page and a short second one. */
const HISTORY_PAGE = 20;
const HISTORY_RECEIPTS = 25;
const FIRST_DAY = Date.UTC(2026, 3, 20); // 2026-04-20; the 25th receipt lands on the fixed visual date 2026-05-14.
const historyDates = () => Array.from({ length: HISTORY_RECEIPTS },
  (_, index) => new Date(FIRST_DAY + index * 86_400_000).toISOString().slice(0, 10));

/**
 * A daily fixed item completed once per day through the public completion API. Receipts are recorded one after
 * another in date order, so the newest receipt is also the latest date and the history order is deterministic.
 */
export async function seedPagedHistory(page: Page, fixture: Fixture): Promise<VisualItem> {
  const title = 'Zalít bylinky na balkoně';
  const subjectId = fixture.subjects.find((subject) => subject.name === 'Domácnost')?.id;
  expect(subjectId).toBeTruthy();
  const created = await api(page, 'post', `${fixture.apiRoot}/items`, {
    title, subjectId, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: 'fixed' },
  }, { 'Idempotency-Key': idempotencyKey() });
  expect(created.status(), 'create history item').toBe(201);
  const item = { id: (await created.json() as { id: string }).id, title };
  for (const completedOn of historyDates()) {
    const current = await api(page, 'get', `${fixture.apiRoot}/items/${item.id}`);
    expect(current.status()).toBe(200);
    const done = await api(page, 'post', `${fixture.apiRoot}/items/${item.id}/completions`, { completedOn },
      { 'Idempotency-Key': idempotencyKey(), 'If-Match': current.headers()['etag'] ?? '' });
    expect(done.status(), `record completion ${completedOn}`).toBe(201);
  }
  return item;
}

/** The settled history page: exactly its receipts, newest first, and the page it states in its own navigation. */
export async function assertHistoryPage(page: Page, locale: Locale, pageNumber: 1 | 2) {
  const catalog = catalogFor(locale);
  const section = page.getByRole('main').getByRole('heading', { level: 2, name: catalog['itemDetail.history'], exact: true })
    .locator('xpath=ancestor::section[1]');
  const display = (value: string) => new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-US', { dateStyle: 'medium', timeZone: 'UTC' })
    .format(new Date(`${value}T00:00:00Z`)).replace(/\s/g, '\u00a0');
  const newestFirst = historyDates().reverse().slice((pageNumber - 1) * HISTORY_PAGE, pageNumber * HISTORY_PAGE);
  await expect(section.getByRole('listitem')).toHaveText(newestFirst.map((value) =>
    new RegExp(`^${escapeRegExp(catalog['itemDetail.historyDone'].replace('{date}', display(value)))}`)));
  const pages = section.getByRole('navigation', { name: catalog['itemDetail.historyPages'] });
  await expect(pages.getByRole('status')).toHaveText(catalog['itemDetail.historyPage'].replace('{page}', String(pageNumber)));
  const button = (key: 'itemDetail.historyOlder' | 'itemDetail.historyNewer' | 'itemDetail.historyLatest') =>
    pages.getByRole('button', { name: catalog[key], exact: true });
  await expect(button('itemDetail.historyOlder')).toHaveCount(pageNumber === 1 ? 1 : 0);
  await expect(button('itemDetail.historyNewer')).toHaveCount(pageNumber === 1 ? 0 : 1);
  await expect(button('itemDetail.historyLatest')).toHaveCount(pageNumber === 1 ? 0 : 1);
  return { older: button('itemDetail.historyOlder') };
}

/** Archive is All items filtered to archived: the stated filter, the complete archived range and only archived rows. */
export async function assertArchiveSettled(page: Page, locale: Locale, total: number) {
  const catalog = catalogFor(locale);
  const main = page.getByRole('main');
  await expect(main.getByRole('heading', { level: 1, name: catalog['allItems.title'] })).toBeVisible();
  const status = main.getByRole('combobox', { name: catalog['allItems.status.label'] });
  await expect(status.locator('option:checked')).toHaveText(`${catalog['allItems.status.archived']} (${total})`);
  const range = catalog['allItems.range'].replace('{from}', '1').replace('{to}', String(total)).replace('{total}', String(total));
  await expect(main.getByRole('heading', { level: 2, name: range })).toBeVisible();
  const rows = main.locator('li[data-item-id]');
  await expect(rows).toHaveCount(total);
  await expect(rows.locator('[data-item-state-kind="archived"]')).toHaveCount(total);
  await expect(main.getByText(catalog['items.subjectLabels.loading'], { exact: true })).toHaveCount(0);
}

/** Phones keep Archive in the More sheet; it must be the current page there while the archived list is shown. */
export async function openMoreSheet(page: Page, locale: Locale) {
  const catalog = catalogFor(locale);
  const nav = page.getByRole('navigation', { name: catalog['nav.label'] });
  const more = nav.getByRole('button', { name: catalog['nav.more'], exact: true });
  await more.click();
  await expect(more).toHaveAttribute('aria-expanded', 'true');
  const sheet = page.locator(`[id="${await more.getAttribute('aria-controls')}"]`);
  const archive = sheet.getByRole('link', { name: catalog['nav.archive'], exact: true });
  await expect(archive).toBeVisible();
  await expect(archive).toHaveAttribute('aria-current', 'page');
  // A press on the sheet's own padding keeps it open (useDismiss) and moves focus off the More button, so the
  // capture's blur cannot read as focus leaving the sheet. The top corners are rounded, so press the centre of
  // the top padding, above the first link.
  const box = await sheet.boundingBox();
  expect(box, 'More sheet is laid out').not.toBeNull();
  await sheet.click({ position: { x: box!.width / 2, y: 4 } });
  await expect(more).toHaveAttribute('aria-expanded', 'true');
  return { sheet, more };
}
