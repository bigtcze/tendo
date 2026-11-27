import { chromium, expect, type Browser, type Page } from '@playwright/test';

type Part = 'day' | 'month' | 'year';

/**
 * How an empty date field reads to a person using each app language: Czech is day-first with Czech letters
 * (`dd.mm.rrrr`, rok = year), the en-US browser the suites use is month-first (`mm/dd/yyyy`).
 */
const EXPECTED: Record<string, { order: Part[]; placeholder: string }> = {
  cs: { order: ['day', 'month', 'year'], placeholder: 'dd.mm.rrrr' },
  en: { order: ['month', 'day', 'year'], placeholder: 'mm/dd/yyyy' },
};

/**
 * A native `<input type="date">` lays out and labels its day, month and year fields in the browser's own UI
 * language. Neither the page `lang` nor the context locale changes that in Chromium, so a Czech capture taken in an
 * English browser showed `mm/dd/yyyy` (#459 design finding F2) where a Czech person's browser shows `dd.mm.rrrr`.
 * Czech captures therefore run in a browser whose UI language is Czech. The full Chromium build is used because
 * the headless shell has no translated UI strings: it orders the fields day-first but still writes `yyyy`.
 */
export async function launchCzechBrowser(): Promise<Browser> {
  return chromium.launch({ channel: 'chromium', env: { ...process.env, LANGUAGE: 'cs' } });
}

/**
 * The default browser of the suites that assert date fields runs with an English UI whatever the host's own
 * language, so their English captures read `mm/dd/yyyy` on every machine (`playwright.config.ts`).
 */
export const ENGLISH_BROWSER_ENV = { LANGUAGE: 'en_US' } as const;

/**
 * Every rendered date field lays out its parts in the order the page language reads them, and an empty one shows
 * that language's placeholder. The parts live in the control's user-agent shadow tree, which only the DevTools
 * protocol can read. Returns how many fields it checked, so a state that must show a date field can prove one was.
 */
export async function assertDateOrderFollowsLanguage(page: Page, label: string): Promise<number> {
  const language = await page.evaluate(() => document.documentElement.lang);
  const session = await page.context().newCDPSession(page);
  try {
    const { root } = await session.send('DOM.getDocument', { depth: -1, pierce: true });
    type DomNode = typeof root;
    type Field = { name: string; backendNodeId: number; parts: Part[]; text: string };
    const fields: Field[] = [];
    const attribute = (node: DomNode, name: string) => {
      const list = node.attributes ?? [];
      for (let index = 0; index < list.length; index += 2) if (list[index] === name) return list[index + 1];
      return undefined;
    };
    const walk = (node: DomNode, field?: Field) => {
      let current = field;
      if (node.nodeName === 'INPUT' && attribute(node, 'type') === 'date') {
        const entry = { name: attribute(node, 'id') ?? attribute(node, 'name') ?? '', backendNodeId: node.backendNodeId, parts: [] as Part[], text: '' };
        fields.push(entry);
        current = entry;
      }
      const part = attribute(node, 'pseudo')?.match(/^-webkit-datetime-edit-(day|month|year)-field$/)?.[1] as Part | undefined;
      if (current && part) current.parts.push(part);
      if (current && node.nodeType === 3) current.text += node.nodeValue;
      for (const child of [...(node.shadowRoots ?? []), ...(node.children ?? [])]) walk(child, current);
    };
    walk(root);
    let checked = 0;
    for (const field of fields) {
      // A field without a box (closed dialog, collapsed section) is not on screen and is skipped.
      const rendered = await session.send('DOM.getBoxModel', { backendNodeId: field.backendNodeId }).then(() => true, () => false);
      if (!rendered) continue;
      const expected = EXPECTED[language];
      expect(expected, `known page language "${language}" for date field ${field.name} in ${label}`).toBeDefined();
      expect(field.parts, `date field ${field.name} reads in ${language} order in ${label}`).toEqual(expected?.order);
      // A field with a value shows digits; only an empty one shows the placeholder letters.
      if (!/\d/.test(field.text)) {
        expect(field.text.trim(), `empty date field ${field.name} placeholder in ${label}`).toBe(expected?.placeholder);
      }
      checked += 1;
    }
    return checked;
  } finally {
    // A closed or crashed page cannot detach; the original failure is the one worth reporting.
    await session.detach().catch(() => undefined);
  }
}
