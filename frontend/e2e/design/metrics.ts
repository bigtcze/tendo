import type { Page } from '@playwright/test';
import { measureCapture } from '../audit/metrics';

export interface DesignBox { x: number; y: number; width: number; height: number }

/**
 * One design-audit capture in numbers: the existing audit measurements (overflow, smallest text, small targets,
 * DOM size) plus the shell geometry a layout review asks about (banner, main insets, headings, row rhythm,
 * primary action alignment). Measurements are taken in the live layout at the captured viewport.
 */
export interface DesignMetrics {
  horizontalOverflow: boolean;
  overflowOffenders: Array<{ tag: string; text: string; right: number }>;
  smallestVisibleTextPx: number | null;
  smallTargets: {
    count: number;
    offenders: Array<{ role: string; name: string; width: number; height: number }>;
  };
  banner: (DesignBox & { fullViewportWidth: boolean }) | null;
  mainInset: { left: number; right: number } | null;
  h1: { fontSize: number; top: number; left: number } | null;
  firstRowY: number | null;
  rowsFullyAboveFold: number;
  primaryAction: (DesignBox & { containerRightGap: number | null }) | null;
  domElements: number;
}

export async function measureDesign(page: Page): Promise<DesignMetrics> {
  const base = await measureCapture(page);
  const layout = await page.evaluate(() => {
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const toBox = (rect: DOMRect) => ({
      x: Math.round(rect.x), y: Math.round(rect.y), width: Math.round(rect.width), height: Math.round(rect.height),
    });
    // The app top bar is the only header outside a dialog; dialog headers are not page chrome.
    const headerRect = [...document.querySelectorAll('header')]
      .find((element) => !element.closest('dialog'))?.getBoundingClientRect() ?? null;
    const mainRect = document.querySelector('main')?.getBoundingClientRect() ?? null;
    const heading = [...document.querySelectorAll<HTMLElement>('h1')]
      .find((element) => element.getBoundingClientRect().width > 0) ?? null;
    const rowRects = [...document.querySelectorAll<HTMLElement>('main li[data-item-id]')]
      .map((row) => row.getBoundingClientRect());
    // The primary action is the design system's primary button (data-variant), never a quiet action.
    const primary = [...document.querySelectorAll<HTMLElement>('main [data-variant="primary"]')]
      .find((element) => element.getBoundingClientRect().width > 0 && element.getBoundingClientRect().height > 0) ?? null;
    const primaryRect = primary?.getBoundingClientRect() ?? null;
    const primaryContainerRect = primary?.parentElement?.getBoundingClientRect() ?? null;
    const overflowOffenders = [...document.querySelectorAll<HTMLElement>('*')]
      .filter((element) => element.getBoundingClientRect().right > document.documentElement.clientWidth + 0.5)
      .slice(0, 10)
      .map((element) => ({
        tag: element.tagName.toLowerCase(),
        text: (element.innerText || element.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 80),
        right: Math.round(element.getBoundingClientRect().right),
      }));
    return {
      banner: headerRect
        ? { ...toBox(headerRect), fullViewportWidth: Math.abs(headerRect.width - viewportWidth) <= 1 }
        : null,
      mainInset: mainRect
        ? { left: Math.round(mainRect.left), right: Math.round(viewportWidth - mainRect.right) }
        : null,
      h1: heading
        ? { fontSize: Number.parseFloat(getComputedStyle(heading).fontSize), top: Math.round(heading.getBoundingClientRect().top), left: Math.round(heading.getBoundingClientRect().left) }
        : null,
      firstRowY: rowRects.length > 0 ? Math.round(rowRects[0]!.top) : null,
      rowsFullyAboveFold: rowRects.filter((rect) => rect.bottom <= viewportHeight + 0.5).length,
      primaryAction: primaryRect
        ? { ...toBox(primaryRect), containerRightGap: primaryContainerRect ? Math.round(primaryContainerRect.right - primaryRect.right) : null }
        : null,
      overflowOffenders,
    };
  });
  return {
    horizontalOverflow: base.horizontalOverflow,
    overflowOffenders: layout.overflowOffenders,
    smallestVisibleTextPx: base.smallestVisibleFontPx,
    smallTargets: { count: base.interactiveTargetsUnder44, offenders: base.targetOffenders.slice(0, 5) },
    banner: layout.banner,
    mainInset: layout.mainInset,
    h1: layout.h1,
    firstRowY: layout.firstRowY,
    rowsFullyAboveFold: layout.rowsFullyAboveFold,
    primaryAction: layout.primaryAction,
    domElements: base.domElements,
  };
}
