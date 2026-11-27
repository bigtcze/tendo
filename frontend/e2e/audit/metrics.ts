import { expect, type Page } from '@playwright/test';
import { measureTargets, pressedBelow } from '../targetSize';

/** The UX audit also counts custom widgets that take focus through tabindex, not only native controls. */
const AUDIT_TARGET_SELECTOR = 'a,button,input,select,textarea,[role="button"],[tabindex]:not([tabindex="-1"])';

export interface CaptureMetrics {
  smallestVisibleFontPx: number | null;
  interactiveTargetsUnder44: number;
  interactiveTargetsUnder24: number;
  targetOffenders: Array<{ role: string; name: string; width: number; height: number }>;
  domElements: number;
  horizontalOverflow: boolean;
  tabStopsToFirstResult: number | null;
}

export async function measureCapture(page: Page, firstResultSelector?: string): Promise<CaptureMetrics> {
  const layout = await page.evaluate(() => {
    const visible = (element: Element): element is HTMLElement => {
      const node = element as HTMLElement;
      const rect = node.getBoundingClientRect();
      const style = getComputedStyle(node);
      const inViewport = rect.right > 0 && rect.bottom > 0 && rect.left < window.innerWidth && rect.top < window.innerHeight;
      const rendered = style.visibility !== 'hidden' && style.display !== 'none' && Number(style.opacity) > 0;
      return rect.width > 0 && rect.height > 0 && inViewport && rendered;
    };
    const hasOwnText = (element: HTMLElement) =>
      [...element.childNodes].some(node => node.nodeType === Node.TEXT_NODE && node.textContent?.trim());
    const textNodes = [...document.querySelectorAll<HTMLElement>('body *')].filter(visible).filter(hasOwnText);
    const fontSizes = textNodes.map(element => Number.parseFloat(getComputedStyle(element).fontSize)).filter(Number.isFinite);
    return { fontSizes, domElements: document.getElementsByTagName('*').length, horizontalOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth };
  });
  // The pressed box comes from the shared target-size predicate (targetSize.ts), the same rule as the required
  // journey checks: a stretched row link is measured by its row, every other control by its label or itself.
  const interactive = (await page.locator(AUDIT_TARGET_SELECTOR).evaluateAll(measureTargets)).filter((target) => target.rendered && target.inViewport);
  const shortfalls = interactive.filter((target) => pressedBelow(target, 44));
  const offenders = shortfalls.map((target) => ({
    role: target.role || target.tag.toLowerCase(),
    name: (target.ariaLabel || target.innerText || target.placeholder || '').trim().replace(/\s+/g, ' ').slice(0, 80),
    width: Math.round(target.pressed.width),
    height: Math.round(target.pressed.height),
  }));
  const visual = {
    smallestVisibleFontPx: layout.fontSizes.length ? Math.min(...layout.fontSizes) : null,
    interactiveTargetsUnder44: offenders.length,
    interactiveTargetsUnder24: shortfalls.filter((target) => pressedBelow(target, 24)).length,
    targetOffenders: offenders.slice(0, 8),
    domElements: layout.domElements,
    horizontalOverflow: layout.horizontalOverflow,
  };
  let tabStopsToFirstResult: number | null = null;
  if (firstResultSelector) {
    // blur() keeps Chromium's sequential focus starting point at the last focused element; clicking an inert
    // top-left point of the page resets it so every capture counts Tab stops from the document start.
    await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); window.scrollTo(0, 0); });
    expect(await page.evaluate(() => document.elementFromPoint(1, 1)?.closest('a,button,input,select,textarea,label,[tabindex]'))).toBeNull();
    await page.mouse.click(1, 1);
    for (let stop = 1; stop <= 100; stop++) {
      await page.keyboard.press('Tab');
      if (await page.locator(firstResultSelector).evaluateAll((els) => els.some(element => element === document.activeElement))) {
        tabStopsToFirstResult = stop;
        break;
      }
    }
  }
  return { ...visual, tabStopsToFirstResult };
}
