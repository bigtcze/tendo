import { expect, test } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { en } from '../src/i18n/en';
import { REFERENCE_HOUSEHOLD, REFERENCE_OWNER_LOGIN, REFERENCE_TITLES, seedReferenceFixture } from './referenceFixture';

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; run scripts/e2e-smoke.sh with TENDO_README_SCREENSHOT`);
  return value;
}

const setupToken = required('E2E_SETUP_TOKEN');
const ownerPassword = required('E2E_OWNER_PASSWORD');
const screenshotPath = resolve(required('E2E_SCREENSHOT_PATH'));

test('capture populated README household home screen', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(setupToken);
  await page.getByLabel(en['onboarding.householdName']).fill(REFERENCE_HOUSEHOLD);
  await page.locator('#onboarding-login').fill(REFERENCE_OWNER_LOGIN);
  await page.locator('#onboarding-password').fill(ownerPassword);
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await expect(page.getByRole('button', { name: en['household.menu.label'].replace('{name}', REFERENCE_HOUSEHOLD) })).toBeVisible();

  const sessionResponse = await page.request.get('/api/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const { defaultHouseholdId: householdId } = await sessionResponse.json() as { defaultHouseholdId: string };
  expect(householdId).toMatch(/^[0-9a-f-]{36}$/);
  const apiRoot = `/api/v1/households/${householdId}`;
  const fixture = await seedReferenceFixture(page, { householdId, apiRoot, ownerPassword });

  await page.reload();
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.waitForLoadState('networkidle');
  await expect(page.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  for (const heading of [en['items.group.needs'], en['items.group.inProgress'], en['items.group.waiting'], en['items.group.upcoming']]) {
    await expect(page.getByRole('heading', { level: 2, name: new RegExp(`^${escapeRegExp(heading)}(\\s|$)`) })).toBeVisible();
  }
  for (const [index, title] of REFERENCE_TITLES.entries()) {
    await expect(page.getByText(title, { exact: true })).toBeVisible();
    expect(fixture.items[index]?.title).toBe(title);
  }
  // Each group states its status once and carries its own count on the heading line.
  const countLabel = (count: number) => (count === 1 ? en['items.group.count.one'] : en['items.group.count.other']).replace('{count}', String(count));
  await expect(page.getByRole('main').getByRole('heading', { level: 2 })).toHaveText([
    `${en['items.group.needs']} ${countLabel(4)}`,
    `${en['items.group.inProgress']} ${countLabel(2)}`,
    `${en['items.group.waiting']} ${countLabel(1)}`,
    `${en['items.group.upcoming']} ${countLabel(3)}`,
  ]);
  // Rows keep the real subject pill, attention date and responsible member as separate readable facts.
  const main = page.getByRole('main');
  await expect(main.getByText(en['items.responsible.read'].replace('{name}', 'anna_novak')).first()).toBeVisible();
  await expect(main.getByText(en['items.responsible.you']).first()).toBeVisible();

  mkdirSync(dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  // Phone companion image: the first screen a household member sees on a phone, next to the desktop path.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect.poll(() => page.evaluate(() => window.innerWidth)).toBe(390);
  await expect(page.getByRole('navigation', { name: en['nav.label'] })).toBeVisible();
  await expect(page.getByText("Book Anna's dental check-up", { exact: true })).toBeVisible();
  await page.screenshot({ path: screenshotPath.replace(/\.png$/, '-phone.png') });
});
