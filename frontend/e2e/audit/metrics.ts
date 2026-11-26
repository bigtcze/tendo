import { expect, type Page } from '@playwright/test';

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
  const visual = await page.evaluate(() => {
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
    const interactiveSelector = 'a,button,input,select,textarea,[role="button"],[tabindex]:not([tabindex="-1"])';
    const interactive = [...document.querySelectorAll<HTMLElement>(interactiveSelector)].filter(visible);
    // The box a reader actually presses. A stretched link (Home rows) pins an absolutely positioned `::after` to all
    // four edges of its positioned row, so a press anywhere on the row opens it: the row, not the 22 px text line, is
    // the target. A partial overlay (a dot, a corner badge) is not pinned on every side and changes nothing. This is
    // the same rule as the required journey n21 target-size predicate. A link that is positioned itself is the overlay's
    // containing block, so the overlay covers only the link and the link is measured.
    const pressedArea = (element: HTMLElement) => {
      const overlay = getComputedStyle(element, '::after');
      const pinned = getComputedStyle(element).position === 'static' && overlay.position === 'absolute' && [overlay.top, overlay.right, overlay.bottom, overlay.left].every(side => side === '0px');
      const covering = pinned ? element.offsetParent : null;
      return covering && covering !== document.body ? covering : element;
    };
    const offenders = interactive.map(element => {
      const rect = pressedArea(element).getBoundingClientRect();
      const label = element.getAttribute('aria-label') || element.innerText || element.getAttribute('placeholder') || '';
      return {
        role: element.getAttribute('role') || element.tagName.toLowerCase(),
        name: label.trim().replace(/\s+/g, ' ').slice(0, 80),
        width: Math.round(rect.width),
        height: Math.round(rect.height),
      };
    }).filter(item => item.width < 44 || item.height < 44);
    return {
      smallestVisibleFontPx: fontSizes.length ? Math.min(...fontSizes) : null,
      interactiveTargetsUnder44: offenders.length,
      interactiveTargetsUnder24: offenders.filter(item => item.width < 24 || item.height < 24).length,
      targetOffenders: offenders.slice(0, 8),
      domElements: document.getElementsByTagName('*').length,
      horizontalOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    };
  });
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
