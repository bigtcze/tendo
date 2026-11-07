import { expect, type Locator, type Page } from '@playwright/test';

export async function capture(page: Page, name: string, width: number, height = 900, fullPage = false, mask: Locator[] = []) {
  await page.setViewportSize({ width, height });
  expect(await page.evaluate(() => window.innerWidth), `viewport for ${name}`).toBe(width);
  await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); window.scrollTo(0, 0); });
  await page.mouse.move(0, 0);
  const started = performance.now();
  await expect(page).toHaveScreenshot(`${name}.png`, {
    animations: 'disabled', caret: 'hide', fullPage, mask,
    maskColor: '#ff00ff', maxDiffPixels: 0,
  });
  console.info(`visual-capture name=${name} width=${width} fullPage=${fullPage} durationMs=${Math.round(performance.now() - started)}`);
}

