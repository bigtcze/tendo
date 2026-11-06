import { expect, test, type Page } from '@playwright/test';
import { en } from '../src/i18n/en';
import { expectNoAxeViolations } from './a11y';

const password = process.env.E2E_OWNER_PASSWORD;
if (!password) throw new Error('E2E_OWNER_PASSWORD is required for cross-browser smoke');

async function ready(page: Page, heading: string) {
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
  await expect(page.getByRole('main')).toBeVisible();
}

test('owner journeys stay accessible in Firefox and WebKit', async ({ page, browserName }) => {
  test.setTimeout(180_000);
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
  await page.getByLabel(en['login.login'], { exact: true }).fill('owner_e2e');
  await page.getByLabel(en['login.password']).fill(password!);
  await page.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(page.getByRole('navigation', { name: en['nav.label'] })).toBeVisible();

  const uniqueTitle = (width: number) => `Cross-browser ${browserName} ${width} ${Date.now()}`;
  const assertViewport = async (path: string, heading: string, width: number, theme: 'light' | 'dark') => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto(path);
    await ready(page, heading);
    if (theme === 'dark') await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    else await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth), `${browserName} ${path} ${theme} ${width}px overflow`).toBeLessThanOrEqual(0);
    await expectNoAxeViolations(page, `${browserName}-${path.replaceAll('/', '-')}-${theme}-${width}`);
    await page.evaluate(() => { (document.activeElement as HTMLElement | null)?.blur(); window.scrollTo(0, 0); });
    await page.keyboard.press('Tab');
    const focus = await page.evaluate(() => {
      const element = document.activeElement as HTMLElement;
      const style = getComputedStyle(element);
      const rect = element.getBoundingClientRect();
      return { name: element.getAttribute('aria-label') || element.textContent?.trim(), visible: rect.width > 0 && rect.height > 0, indicator: `${style.outlineStyle}/${style.outlineWidth}/${style.outlineColor}/${style.boxShadow}` };
    });
    expect(focus.visible, `${browserName} ${path} first tab target visible`).toBe(true);
    expect(focus.indicator, `${browserName} ${path} first tab changes visible focus`).not.toBe('none/0px/rgb(0, 0, 0)/none');
    if (path === '/') {
      const primary = page.getByRole('main').getByRole('button', { name: en['items.add'], exact: true });
      for (let stop = 0; stop < 20 && !(await primary.evaluate((element) => element === document.activeElement)); stop += 1) await page.keyboard.press('Tab');
      await expect(primary, `${browserName} Home keyboard pass reaches primary add action`).toBeFocused();
    }
  };

  const routeChecks = [
    { path: '/', heading: 'Veselí' },
    { path: '/items', heading: en['allItems.title'] },
    { path: '/people', heading: en['subjects.title'] },
    { path: '/members', heading: en['members.title'] },
    { path: '/account', heading: en['account.title'] },
  ];
  for (const width of [390, 1280]) {
    for (const route of routeChecks) await assertViewport(route.path, route.heading, width, 'light');
    await page.setViewportSize({ width, height: 844 });
    await page.goto('/items');
    await ready(page, en['allItems.title']);
    const search = page.getByRole('searchbox', { name: en['allItems.search'], exact: true });
    await search.fill('Water');
    await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
    const link = page.getByRole('region', { name: en['allItems.results'] }).getByRole('link').first();
    await expect(link).toBeVisible();
    const detailTitle = (await link.innerText()).trim();
    const detailPath = await link.getAttribute('href');
    expect(detailPath).toMatch(/^\/items\//);
    await page.goto('/');
    await assertViewport('/', 'Veselí', width, 'light');
    await page.getByRole('button', { name: en['items.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    const itemTitle = uniqueTitle(width);
    await form.getByLabel(en['items.title'], { exact: true }).fill(itemTitle);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ index: 1 });
    await form.getByRole('button', { name: en['items.add.submit'], exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: itemTitle })).toContainText(itemTitle);
    const session = await page.request.get('/api/v1/session');
    expect(session.status()).toBe(200);
    const { defaultHouseholdId } = await session.json() as { defaultHouseholdId: string };
    const activeItems = await page.request.get(`/api/v1/households/${defaultHouseholdId}/items?limit=100&done=false`);
    expect(activeItems.status()).toBe(200);
    const createdItem = ((await activeItems.json()) as { items: Array<{ id: string; title: string; done: boolean }> }).items.find((item) => item.title === itemTitle && !item.done);
    expect(createdItem, `${browserName} created item persists after add`).toBeDefined();
    const itemId = createdItem!.id;
    await page.goto(`/items/${itemId}`);
    await expect(page.getByRole('heading', { level: 1, name: itemTitle })).toBeVisible();
    await page.getByRole('button', { name: en['items.done.label'].replace('{title}', itemTitle), exact: true }).click();
    await expect(page.getByRole('button', { name: en['items.undo'], exact: true })).toBeVisible();
    const undoRequest = page.waitForResponse((response) => response.url().includes(`/items/${itemId}/completions/`) && response.request().method() === 'PATCH');
    await page.getByRole('button', { name: en['items.undo'], exact: true }).click();
    expect((await undoRequest).status(), `${browserName} undo accepted by API`).toBe(200);
    const persistedItem = await page.request.get(`/api/v1/households/${defaultHouseholdId}/items/${itemId}`);
    expect(persistedItem.status()).toBe(200);
    expect((await persistedItem.json() as { title: string; done: boolean })).toMatchObject({ title: itemTitle, done: false });
    await assertViewport(detailPath!, detailTitle, width, 'light');
  }
  // Change appearance using the actual Account control, then verify all required routes at both widths.
  await page.goto('/account');
  await ready(page, en['account.title']);
  await page.getByRole('main').getByRole('group', { name: en['account.appearance.title'], exact: true }).getByRole('button', { name: en['account.appearance.dark'], exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  for (const width of [390, 1280]) for (const route of routeChecks) await assertViewport(route.path, route.heading, width, 'dark');
});
