import { expect, type Locator, type Page } from '@playwright/test';

/** Fonts must be ready before any pixel or geometry measurement that depends on line boxes. */
export async function assertInterReady(page: Page, label = 'capture') {
  const font = await page.evaluate(async () => {
    await document.fonts.ready;
    return {
      family: getComputedStyle(document.body).fontFamily,
      loaded: [...document.fonts].some((face) => face.family.includes('Inter Variable') && face.status === 'loaded'),
    };
  });
  expect(font.family, `Inter Variable body font for ${label}`).toContain('Inter Variable');
  expect(font.loaded, `Inter Variable face loaded for ${label}`).toBe(true);
}

/**
 * Every rendered text field and select is at least 16 px, so iOS Safari never zooms the page when one takes focus
 * (UX.md "Accessibility"; `--text-control`). Checkboxes and radios carry no text and are skipped.
 */
export async function assertFieldsAtLeast16px(page: Page, label: string) {
  const small = await page.evaluate(() => [...document.querySelectorAll('input, select, textarea')]
    .filter((field) => !(field instanceof HTMLInputElement && ['checkbox', 'radio'].includes(field.type)))
    .filter((field) => field.getClientRects().length > 0)
    .map((field) => ({
      field: `${field.tagName.toLowerCase()}[${field.getAttribute('aria-label')
        ?? (field as HTMLInputElement).labels?.[0]?.firstChild?.textContent?.trim() ?? (field.id || field.getAttribute('name')) ?? ''}]`,
      px: parseFloat(getComputedStyle(field).fontSize),
    }))
    .filter(({ px }) => px < 16));
  expect(small, `fields under 16 px in ${label}`).toEqual([]);
}

/**
 * Every rendered select draws the app's chevron instead of the platform arrow: `appearance: none` on the select and
 * one visible, pointer-transparent chevron inside its right edge, clear of the option text (#459 design finding).
 */
export async function assertSelectsUseAppChevron(page: Page, label: string) {
  const selects = await page.evaluate(() => [...document.querySelectorAll('select')]
    .filter((select) => select.getClientRects().length > 0)
    .map((select) => {
      const box = select.getBoundingClientRect();
      const chevrons = [...(select.parentElement?.querySelectorAll(':scope > [data-select-chevron]') ?? [])];
      const chevron = chevrons[0] as SVGElement | undefined;
      const mark = chevron?.getBoundingClientRect();
      const style = getComputedStyle(select);
      return {
        name: select.getAttribute('aria-label') ?? select.labels?.[0]?.firstChild?.textContent?.trim() ?? select.id,
        appearance: style.appearance,
        chevrons: chevrons.length,
        visible: Boolean(chevron?.checkVisibility({ opacityProperty: true, visibilityProperty: true })),
        passive: chevron ? getComputedStyle(chevron).pointerEvents === 'none' : false,
        inside: Boolean(mark && mark.width >= 12 && mark.left >= box.left && mark.right <= box.right - 8
          && mark.top >= box.top && mark.bottom <= box.bottom),
        textClear: Boolean(mark && box.right - parseFloat(style.paddingRight) <= mark.left),
      };
    }));
  for (const select of selects) {
    expect(select, `select ${select.name} uses the app chevron in ${label}`).toMatchObject({
      appearance: 'none', chevrons: 1, visible: true, passive: true, inside: true, textClear: true,
    });
  }
}

export async function capture(page: Page, name: string, width: number, height = 900, fullPage = false, mask: Locator[] = []) {
  await page.setViewportSize({ width, height });
  expect(await page.evaluate(() => window.innerWidth), `viewport for ${name}`).toBe(width);
  await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); window.scrollTo(0, 0); });
  await page.mouse.move(0, 0);
  // Self-hosted Inter must be the rendered font, and the face must be loaded before the pixels are taken.
  await assertInterReady(page, name);
  await assertFieldsAtLeast16px(page, name);
  await assertSelectsUseAppChevron(page, name);
  const started = performance.now();
  await expect(page).toHaveScreenshot(`${name}.png`, {
    animations: 'disabled', caret: 'hide', fullPage, mask,
    maskColor: '#ff00ff', maxDiffPixels: 0,
  });
  console.info(`visual-capture name=${name} width=${width} fullPage=${fullPage} durationMs=${Math.round(performance.now() - started)}`);
}
