import type { Page } from '@playwright/test';

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
    const offenders = interactive.map(element => {
      const rect = element.getBoundingClientRect();
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
    await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); window.scrollTo(0, 0); });
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
