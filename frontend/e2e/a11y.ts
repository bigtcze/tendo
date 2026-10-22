import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

// WCAG 2.2 AA-oriented automated scan. Automated rules cover only part of WCAG; keyboard,
// focus, zoom and reduced-motion behavior are asserted separately in the journey.
export const wcagTags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

export async function expectNoAxeViolations(page: Page, label: string) {
  // Entrance animations fade content in from opacity 0; scanning mid-fade measures blended colours
  // that users never see at rest, so scan only once every running animation has finished.
  await page.waitForFunction(() => document.getAnimations().every((animation) => animation.playState !== 'running'));
  const results = await new AxeBuilder({ page }).withTags(wcagTags).analyze();
  const summary = results.violations.map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    help: violation.help,
    targets: violation.nodes.map((node) => `${node.target.join(' ')} :: ${node.any[0]?.message ?? node.failureSummary ?? ''}`),
  }));
  await test.info().attach(`axe-${label}.json`, {
    body: JSON.stringify({ url: page.url(), violations: summary }, null, 2),
    contentType: 'application/json',
  });
  // Guard against a scan that silently checked nothing.
  expect(results.passes.length, `axe ran no passing rules on ${label}`).toBeGreaterThan(0);
  expect(summary, `axe WCAG violations on ${label}`).toEqual([]);
}
