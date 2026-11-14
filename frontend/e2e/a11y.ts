import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

// WCAG 2.2 AA-oriented automated scan. Automated rules cover only part of WCAG; keyboard,
// focus, zoom and reduced-motion behavior are asserted separately in the journey.
export const wcagTags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

type LabelledByStash = { __tendoLabelledByDescriptor?: PropertyDescriptor };

/**
 * The Firefox build bundled with Playwright 1.65 (159.0, r1572) crashes its content process ("Page crashed") once the
 * same element's `ariaLabelledByElements` is read from two JS worlds: Playwright's utility world reads it for role and
 * label locators, axe reads it in the page world (Mozilla bug 2090055, microsoft/playwright#43967, Tendo #493).
 * During a Firefox scan the page-world getter returns null, so axe resolves `aria-labelledby` from the attribute, the
 * path it already takes when the property is absent. Tendo never sets element references from script, so the scan
 * checks the same relations. Playwright's own utility world keeps the native getter. Remove this once the locked
 * Playwright bundles a fixed Firefox (r1578 from the 1.66 line no longer crashes on the reproduction): the #493
 * regression test in cross-browser.spec.ts then passes without it.
 */
async function withoutPageWorldLabelledByReflection<T>(page: Page, run: () => Promise<T>): Promise<T> {
  if (page.context().browser()?.browserType().name() !== 'firefox') return run();
  await page.evaluate(() => {
    const native = Object.getOwnPropertyDescriptor(Element.prototype, 'ariaLabelledByElements');
    if (!native) return;
    (window as LabelledByStash).__tendoLabelledByDescriptor = native;
    Object.defineProperty(Element.prototype, 'ariaLabelledByElements', { ...native, get: () => null });
  });
  // A failed or crashed scan fails the test and its page is discarded, so restore only after success: restoring in a
  // `finally` would replace the scan's own error with a second one from a dead page.
  const result = await run();
  await page.evaluate(() => {
    const native = (window as LabelledByStash).__tendoLabelledByDescriptor;
    if (!native) return;
    Object.defineProperty(Element.prototype, 'ariaLabelledByElements', native);
    delete (window as LabelledByStash).__tendoLabelledByDescriptor;
  });
  return result;
}

/** Runs the WCAG-tagged axe scan on the settled page and returns the raw results. */
export async function analyzeWcag(page: Page) {
  // Entrance animations fade content in from opacity 0; scanning mid-fade measures blended colours
  // that users never see at rest, so scan only once every running animation has finished.
  await page.waitForFunction(() => document.getAnimations().every((animation) => animation.playState !== 'running'));
  return withoutPageWorldLabelledByReflection(page, () => new AxeBuilder({ page }).withTags(wcagTags).analyze());
}

export async function expectNoAxeViolations(page: Page, label: string) {
  const results = await analyzeWcag(page);
  const summary = results.violations.map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    help: violation.help,
    targets: violation.nodes.map((node) => `${node.target.join(' ')} :: ${node.any[0]?.message ?? node.failureSummary ?? ''}`),
  }));
  // Keep incomplete results (needs manual review, e.g. contrast over images) and passing rule ids as evidence.
  const incomplete = results.incomplete.map((rule) => ({ id: rule.id, impact: rule.impact, help: rule.help, nodes: rule.nodes.map((node) => ({ target: node.target.join(' '), any: node.any.map((check) => check.message), all: node.all.map((check) => check.message), none: node.none.map((check) => check.message) })) }));
  await test.info().attach(`axe-${label}.json`, {
    body: JSON.stringify({ url: page.url(), violations: summary, incomplete, passes: results.passes.map((rule) => rule.id) }, null, 2),
    contentType: 'application/json',
  });
  // Printed so the evidence survives in the runner's process log (attachments are not kept for passing runs).
  if (incomplete.length) console.log(`axe-incomplete ${label} ${JSON.stringify(incomplete)}`);
  // Guard against a scan that silently checked nothing.
  expect(results.passes.length, `axe ran no passing rules on ${label}`).toBeGreaterThan(0);
  expect(summary, `axe WCAG violations on ${label}`).toEqual([]);
}
