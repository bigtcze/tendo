import { expect, type Page } from '@playwright/test';
import { THEME_STORAGE_KEY } from '../../src/theme/theme';

/**
 * Switches the page to the saved Dark preference the way a user does it: the stored choice is written and the page
 * reloads, so the theme-boot script applies it before first paint. Replaces attribute-only "forced dark" hacks, which
 * skipped the preference path and the first-paint script this audit is meant to evidence.
 */
export async function applySavedDarkPreference(page: Page) {
  await page.evaluate((key) => localStorage.setItem(key, 'dark'), THEME_STORAGE_KEY);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
}

/** Clears the saved choice so the page follows the emulated device again (System). */
export async function applySystemAppearance(page: Page) {
  await page.evaluate((key) => localStorage.removeItem(key), THEME_STORAGE_KEY);
  await page.reload();
  await expect(page.locator('html')).not.toHaveAttribute('data-theme');
}
