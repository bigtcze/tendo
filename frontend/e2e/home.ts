import { expect, type Page } from '@playwright/test';
import { cs } from '../src/i18n/cs';
import { en } from '../src/i18n/en';

const escapeRegExp = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// Home's title is the locale's "Overview" (en) or "Přehled" (cs); the household name lives in the switcher.
const homeTitle = new RegExp(`^(?:${[...new Set([en['home.overview'], cs['home.overview']])].map(escapeRegExp).join('|')})$`);

/**
 * Home is shown for the household: the Overview h1 is visible and the active-household switcher names it.
 * Locale-neutral so one helper serves both en and cs flows.
 */
export async function expectHome(page: Page, householdName: string) {
  await expect(page.getByRole('heading', { level: 1, name: homeTitle })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(`: ${escapeRegExp(householdName)}$`) })).toBeVisible();
}
