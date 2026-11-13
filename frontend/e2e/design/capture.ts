import { expect, type Page } from '@playwright/test';
import { join } from 'node:path';
import { LOCALE_STORAGE_KEY, type Locale, type MessageKey } from '../../src/i18n';
import { cs } from '../../src/i18n/cs';
import { en } from '../../src/i18n/en';
import { THEME_STORAGE_KEY } from '../../src/theme/theme';
import { assertInterReady } from '../visual/capture';
import { measureDesign, type DesignMetrics } from './metrics';

export type Catalog = Record<MessageKey, string>;
export type Theme = 'light' | 'dark';

/** Design-audit widths; the fold height per width follows the shell's own breakpoints. */
export const WIDTHS = [320, 390, 768, 1024, 1440, 1920] as const;
export const LOCALES: readonly Locale[] = ['en', 'cs'];
export const THEMES: readonly Theme[] = ['light', 'dark'];
export const CATALOGS: Record<Locale, Catalog> = { en, cs };
export const heightFor = (width: number) => (width < 768 ? 844 : width === 768 ? 1024 : 900);
export const captureName = (state: string, locale: Locale, theme: Theme, width: number) => `${state}-${locale}-${theme}-${width}`;

/**
 * Every state starts from a fresh document. about:blank avoids same-URL and fragment-only navigations, which the
 * SPA would treat as same-document and therefore keep the previous locale/theme/state on screen.
 */
export async function open(page: Page, path: string) {
  await page.goto('about:blank');
  await page.goto(path);
}

export type Prepare = (page: Page, catalog: Catalog) => Promise<void>;

/** Media queries, fonts and the settle transition all land on frame boundaries; measurements follow one. */
async function nextFrame(page: Page) {
  await page.evaluate(() => new Promise<void>((done) => requestAnimationFrame(() => done())));
}

/** How many frames a reading may need before two consecutive measurements agree. */
const SETTLE_FRAMES = 10;

/**
 * Opt-in design baseline: every prepared state is photographed at each width in both languages and both explicit
 * themes, and every image is paired with measured layout numbers.
 *
 * Language is the app's own persisted preference (localStorage `tendo.locale`), which wins over the browser locale
 * in detectLocale(); theme is the persisted `tendo.theme` choice applied by the public theme-boot script before
 * first paint. Both are written before the state navigates, so the captured document really booted that way.
 */
export class DesignCapture {
  readonly metrics: Record<string, DesignMetrics> = {};
  private readonly dir: string;

  constructor(dir: string) {
    this.dir = dir;
  }

  async state(
    page: Page,
    state: string,
    prepare: Prepare,
    options: { widths?: readonly number[]; fullPageAt?: readonly number[] } = {},
  ) {
    const widths = options.widths ?? WIDTHS;
    const fullPageAt = options.fullPageAt ?? [];
    for (const locale of LOCALES) {
      for (const theme of THEMES) {
        if (!page.url().startsWith('http')) await page.goto('/');
        await page.evaluate((preferences) => {
          localStorage.setItem(preferences.localeKey, preferences.locale);
          localStorage.setItem(preferences.themeKey, preferences.theme);
        }, { localeKey: LOCALE_STORAGE_KEY, themeKey: THEME_STORAGE_KEY, locale, theme });
        await prepare(page, CATALOGS[locale]);
        await expect(page.locator('html'), `${state} ${locale} ${theme} theme`).toHaveAttribute('data-theme', theme);
        await expect(page.getByRole('main'), `${state} ${locale} ${theme} main landmark`).toBeVisible();
        await expect(page.getByRole('heading', { level: 1 }).first(), `${state} ${locale} ${theme} h1`).toBeVisible();
        for (const width of widths) await this.capture(page, state, locale, theme, width, fullPageAt.includes(width));
      }
    }
  }

  private async capture(page: Page, state: string, locale: Locale, theme: Theme, width: number, fullPage: boolean) {
    await page.setViewportSize({ width, height: heightFor(width) });
    // Media queries re-evaluate on the next rendering step, not on forced layout: two frames keep 1440 -> 390 honest.
    await nextFrame(page);
    await nextFrame(page);
    expect(await page.evaluate(() => window.innerWidth), `${state} ${locale} ${theme} viewport width`).toBe(width);
    const name = captureName(state, locale, theme, width);
    await page.evaluate(() => {
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      window.scrollTo(0, 0);
    });
    await page.mouse.move(0, 0);
    await assertInterReady(page, name);
    // Measure only after the faces are ready, and never from a mid-reflow frame: a phone bottom bar or a wrapping
    // toolbar can be photographed one frame before its final row is laid out, which used to record 4x56 targets.
    const metrics = await this.settledMetrics(page, name);
    await page.screenshot({ path: join(this.dir, `${name}.png`), animations: 'disabled', caret: 'hide' });
    this.metrics[name] = metrics;
    if (!fullPage) return;
    // Fixed chrome is painted where it sat when the shot starts, so at the top of the document the shell's bottom bar
    // covers mid-page content in the stitched image. At the document end it lands in its own place instead.
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    await page.screenshot({ path: join(this.dir, `${name}-full.png`), fullPage: true, animations: 'disabled', caret: 'hide' });
    // The stitched image has no single viewport geometry, so this entry keeps the numbers it was measured from and
    // says so, instead of implying they describe the full document.
    this.metrics[`${name}-full`] = { ...metrics, fullPage: true };
  }

  /** Reads the layout until two consecutive frames agree, so an intermediate reflow never becomes the baseline. */
  private async settledMetrics(page: Page, name: string): Promise<DesignMetrics> {
    let measured = await measureDesign(page);
    for (let frame = 0; frame < SETTLE_FRAMES; frame += 1) {
      await nextFrame(page);
      const next = await measureDesign(page);
      if (JSON.stringify(next) === JSON.stringify(measured)) return next;
      measured = next;
    }
    throw new Error(`${name} layout did not settle within ${SETTLE_FRAMES} frames`);
  }
}
