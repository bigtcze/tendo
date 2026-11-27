import { expect, test, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { en } from '../src/i18n/en';
import { designTokens } from '../src/designTokens';
import { THEME_STORAGE_KEY } from '../src/theme/theme';
import { auditContrast, collectContrastCandidates, contrastShortfalls } from './contrastAudit';
import { effectiveThemeColor } from './themeColor';
import { measureCapture } from './audit/metrics';
import { measureTargets, pressedBelow } from './targetSize';

// Self-contained appearance proof for signed-out routes. It creates its own contexts and never touches journey.spec.ts
// state. The Appearance control lives on the signed-in Account screen (journey.spec.ts drives it there), so here a
// saved choice is written exactly as that control writes it, then the first paint is checked.
// Covers System / Light / Dark against an emulated OS scheme: first paint, persistence across reload, color-scheme,
// and live OS switching while the preference is System.

const LIGHT_PAPER = 'rgb(248, 247, 243)';
const DARK_PAPER = 'rgb(23, 22, 20)';
type Scheme = 'light' | 'dark';
type Preference = 'system' | 'light' | 'dark';

async function newContext(browser: Browser, scheme: Scheme): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ colorScheme: scheme, reducedMotion: 'reduce', viewport: { width: 390, height: 844 }, locale: 'en-US' });
  return { context, page: await context.newPage() };
}

/**
 * Records at readyState=interactive: the parser has run the synchronous theme-boot script, but deferred module scripts
 * have not started yet. This observes the app's initial scheme after the preference has been applied and before React
 * mounts. An init script would run before the boot script and miss the saved choice this test proves.
 */
async function recordParserCompletionTheme(page: Page) {
  await page.addInitScript(() => {
    const capture = () => {
      if (document.readyState !== 'interactive') return;
      (window as unknown as { __parserCompletionTheme?: Paint }).__parserCompletionTheme = {
        background: getComputedStyle(document.documentElement).backgroundColor,
        scheme: getComputedStyle(document.documentElement).colorScheme,
        dataTheme: document.documentElement.getAttribute('data-theme'),
        // Inline copy of effectiveThemeColor (./themeColor): init scripts cannot import helpers.
        themeColor: [...document.querySelectorAll('meta[name="theme-color"]')]
          .find((meta) => !meta.getAttribute('media') || window.matchMedia(meta.getAttribute('media')!).matches)
          ?.getAttribute('content') ?? null,
      };
      document.removeEventListener('readystatechange', capture);
    };
    document.addEventListener('readystatechange', capture);
    capture();
  });
}

async function recordRenderedFrames(page: Page) {
  await page.addInitScript(() => {
    const state = window as unknown as { __themeFrames: Array<{ background: string; dataTheme: string | null; time: number }>; __paintEntries: Array<{ name: string; startTime: number }> };
    state.__themeFrames = [];
    state.__paintEntries = [];
    try {
      const observer = new PerformanceObserver((entries) => {
        for (const entry of entries.getEntries()) state.__paintEntries.push({ name: entry.name, startTime: entry.startTime });
      });
      observer.observe({ type: 'paint', buffered: true });
    } catch {
      // Paint timing is supplementary; animation-frame surface readings are the asserted evidence.
    }
    const sample = (time: number) => {
      const root = document.documentElement;
      state.__themeFrames.push({
        background: getComputedStyle(root).backgroundColor,
        dataTheme: root.getAttribute('data-theme'),
        time,
      });
      if (!document.querySelector('#root')?.childElementCount && state.__themeFrames.length < 120) requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
}

async function holdAppModule(page: Page, delayMs = 700) {
  await page.route(/\/assets\/[^/]+\.js(?:\?.*)?$/, async (route) => {
    const response = await route.fetch();
    await new Promise((resolve) => setTimeout(resolve, delayMs));
    await route.fulfill({ response });
  });
}

async function frameEvidence(page: Page) {
  return page.evaluate(() => {
    const state = window as unknown as {
      __themeFrames?: Array<{ background: string; dataTheme: string | null; time: number }>;
      __paintEntries?: Array<{ name: string; startTime: number }>;
    };
    return { frames: state.__themeFrames ?? [], paintEntries: state.__paintEntries ?? [] };
  });
}

async function initSavedPreference(page: Page, preference: Preference) {
  await page.addInitScript(({ key, value }) => {
    if (value === 'system') localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  }, { key: THEME_STORAGE_KEY, value: preference });
}

type Paint = { background: string; scheme: string; dataTheme: string | null; themeColor: string | null };
const readPaint = async (page: Page): Promise<Paint> => ({
  ...await page.evaluate(() => ({
    background: getComputedStyle(document.documentElement).backgroundColor,
    scheme: getComputedStyle(document.documentElement).colorScheme,
    dataTheme: document.documentElement.getAttribute('data-theme'),
  })),
  themeColor: await effectiveThemeColor(page),
});
// The theme-color metas carry the paper tokens.
const LIGHT_TINT = designTokens.light.paper;
const DARK_TINT = designTokens.dark.paper;

/** Writes the choice as the Appearance control does: explicit values are stored, System removes the key. */
async function savePreference(page: Page, preference: Preference) {
  await page.evaluate(({ key, value }) => {
    if (value === 'system') localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  }, { key: THEME_STORAGE_KEY, value: preference });
}

test.describe('appearance preference on signed-out routes', () => {
  test('System, Light and Dark paint the right background at first paint and after reload', async ({ browser }) => {
    for (const scheme of ['light', 'dark'] as const) {
      for (const preference of ['system', 'light', 'dark'] as const) {
        const label = `${preference} on ${scheme} device`;
        const { context, page } = await newContext(browser, scheme);
        await page.goto('/login');
        await savePreference(page, preference);
        await recordParserCompletionTheme(page);
        await page.reload();
        await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();

        const effective = preference === 'system' ? scheme : preference;
        const expectedBackground = effective === 'dark' ? DARK_PAPER : LIGHT_PAPER;
        const first = await page.evaluate(() => (window as unknown as { __parserCompletionTheme?: Paint }).__parserCompletionTheme);
        expect(first, `${label}: parser-completion theme recorded before deferred app mount`).toBeDefined();
        expect(first!.background, `${label}: parser-completion background`).toBe(expectedBackground);
        expect(first!.dataTheme, `${label}: parser-completion data-theme`).toBe(preference === 'system' ? null : preference);
        // The browser chrome tint (theme-color) follows the same effective theme before the app mounts.
        const expectedTint = effective === 'dark' ? DARK_TINT : LIGHT_TINT;
        expect(first!.themeColor, `${label}: parser-completion theme-color`).toBe(expectedTint);
        const after = await readPaint(page);
        expect(after.background, `${label}: background after load`).toBe(expectedBackground);
        expect(after.themeColor, `${label}: theme-color after load`).toBe(expectedTint);
        // Only an explicit choice pins color-scheme; System lets the device pick through light-dark().
        const expectedColorScheme = preference === 'light' ? 'light' : effective === 'dark' ? 'dark' : 'light dark';
        expect(after.scheme, `${label}: color-scheme`).toBe(expectedColorScheme);
        expect(after.dataTheme, `${label}: data-theme after reload`).toBe(preference === 'system' ? null : preference);
        await context.close();
      }
    }
  });

  test('rendered frames stay on the selected surface before app mount, and detect a late/missing boot failure', async ({ browser }) => {
    const cases: Array<{ preference: Preference; scheme: Scheme; expected: string; label: string }> = [
      { preference: 'dark', scheme: 'light', expected: DARK_PAPER, label: 'saved Dark on light OS' },
      { preference: 'light', scheme: 'dark', expected: LIGHT_PAPER, label: 'saved Light on dark OS' },
      { preference: 'system', scheme: 'dark', expected: DARK_PAPER, label: 'System on dark OS' },
    ];
    for (const item of cases) {
      const { context, page } = await newContext(browser, item.scheme);
      await initSavedPreference(page, item.preference);
      await recordRenderedFrames(page);
      await holdAppModule(page);
      await page.goto('/login');
      await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
      const evidence = await frameEvidence(page);
      expect(evidence.frames.length, `${item.label}: captured rendered animation frames`).toBeGreaterThan(0);
      expect(evidence.frames.every((frame) => frame.background === item.expected), `${item.label}: every sampled rendered frame uses expected surface`).toBe(true);
      expect(evidence.frames.some((frame) => frame.dataTheme === (item.preference === 'system' ? null : item.preference)), `${item.label}: frames record expected preference attribute`).toBe(true);
      expect(evidence.paintEntries.some((entry) => entry.name === 'first-paint'), `${item.label}: first-paint timing recorded`).toBe(true);
      await context.close();
    }

    // Negative control: if the same-origin boot script is aborted, saved Dark cannot apply before the app.
    // This proves the frame detector is sensitive to the exact bright frame the boot script prevents.
    const { context, page } = await newContext(browser, 'light');
    await initSavedPreference(page, 'dark');
    await recordRenderedFrames(page);
    await holdAppModule(page);
    await page.route('**/theme-boot.js', (route) => route.abort());
    await page.goto('/login');
    await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    const brokenEvidence = await frameEvidence(page);
    expect(brokenEvidence.frames.some((frame) => frame.background === LIGHT_PAPER), 'negative control catches light frames when theme boot is blocked').toBe(true);
    expect(brokenEvidence.frames.every((frame) => frame.background !== DARK_PAPER), 'negative control never falsely observes saved Dark before module mount').toBe(true);
    await context.close();
  });

  test('a saved choice survives a reload and a tampered stored value falls back to System', async ({ browser }) => {
    const { context, page } = await newContext(browser, 'light');
    await page.goto('/login');
    await savePreference(page, 'dark');
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    expect(await page.evaluate((key) => localStorage.getItem(key), THEME_STORAGE_KEY)).toBe('dark');
    expect(await readPaint(page)).toMatchObject({ background: DARK_PAPER, dataTheme: 'dark' });
    await context.close();

    const tampered = await newContext(browser, 'light');
    await tampered.page.goto('/login');
    await savePreference(tampered.page, 'system');
    await tampered.page.evaluate((key) => localStorage.setItem(key, 'neon'), THEME_STORAGE_KEY);
    await tampered.page.reload();
    await expect(tampered.page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    expect(await readPaint(tampered.page)).toMatchObject({ background: LIGHT_PAPER, dataTheme: null });
    await tampered.context.close();
  });

  test('the production document runs the external boot script before the app module and stylesheet', async ({ request }) => {
    const response = await request.get('/');
    expect(response.status()).toBe(200);
    const html = await response.text();
    const boot = html.indexOf('<script src="/theme-boot.js"></script>');
    const module = html.indexOf('<script type="module"');
    const stylesheet = html.indexOf('rel="stylesheet"');
    expect(boot, 'same-origin classic bootstrap is present').toBeGreaterThan(-1);
    expect(module, 'production module bundle is present').toBeGreaterThan(-1);
    expect(stylesheet, 'production stylesheet is present').toBeGreaterThan(-1);
    expect(boot, 'bootstrap precedes the module bundle').toBeLessThan(module);
    expect(boot, 'bootstrap precedes the stylesheet').toBeLessThan(stylesheet);
    const scripts = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/gi)];
    expect(scripts.filter(([, body]) => body.trim().length > 0), 'document has no inline scripts').toHaveLength(0);
  });

  test('System follows a device change live, and an explicit choice ignores it', async ({ browser }) => {
    const { context, page } = await newContext(browser, 'light');
    await page.goto('/login');
    expect((await readPaint(page)).background).toBe(LIGHT_PAPER);
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect.poll(async () => (await readPaint(page)).background, { message: 'System follows the device to dark' }).toBe(DARK_PAPER);
    expect((await readPaint(page)).themeColor, 'System tint follows the device to dark').toBe(DARK_TINT);
    await savePreference(page, 'light');
    await page.reload();
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect.poll(async () => (await readPaint(page)).background, { message: 'explicit Light keeps light on a dark device' }).toBe(LIGHT_PAPER);
    expect((await readPaint(page)).themeColor, 'explicit Light keeps the light tint on a dark device').toBe(LIGHT_TINT);
    await context.close();
  });
});

test.describe('brand icons in the production build', () => {
  test('the browser gets the SVG icon and a real ICO at /favicon.ico, never a 404 or the app page', async ({ request }) => {
    const html = await (await request.get('/')).text();
    // Parse every icon link (attribute order and formatting may change) and keep them in document order.
    const icons = [...html.matchAll(/<link\b[^>]*>/gi)]
      .map(([tag]) => Object.fromEntries([...tag.matchAll(/([\w-]+)="([^"]*)"/g)].map(([, name, value]) => [name.toLowerCase(), value])))
      .filter((attrs) => attrs.rel?.split(/\s+/).includes('icon'));
    const icoLink = icons.findIndex((attrs) => attrs.href === '/favicon.ico');
    const svgLink = icons.findIndex((attrs) => attrs.href === '/favicon.svg');
    expect(icoLink, 'ICO fallback is linked').toBeGreaterThan(-1);
    expect(icons[icoLink].sizes, 'ICO declares its raster size').toBe('32x32');
    expect(icons[svgLink]?.type, 'SVG icon is typed').toBe('image/svg+xml');
    // The typed SVG comes last so browsers with SVG favicon support choose it over the ICO.
    expect(svgLink, 'SVG icon is linked after the ICO').toBeGreaterThan(icoLink);
    // Browsers without SVG favicon support, and every browser's own /favicon.ico probe, ask for this path.
    const ico = await request.get('/favicon.ico');
    expect(ico.status()).toBe(200);
    expect(ico.headers()['content-type']).toBe('image/x-icon');
    const bytes = await ico.body();
    expect([bytes.readUInt16LE(0), bytes.readUInt16LE(2)], 'ICO header: reserved 0, type icon').toEqual([0, 1]);
    const svg = await request.get('/favicon.svg');
    expect(svg.status()).toBe(200);
    expect(svg.headers()['content-type']).toBe('image/svg+xml');
  });
});

test.describe('contrast auditor surface base', () => {
  test('rejects translucent root surfaces, falls back only to an opaque body, and fails closed otherwise', async ({ page }) => {
    await page.setContent('<html style="background: rgba(1, 2, 3, .5)"><body style="background: rgb(248, 247, 243)"><p>Fallback body</p></body></html>');
    const rows = await collectContrastCandidates(page);
    const paragraph = rows.find((row) => row.kind === 'text' && row.name === 'p');
    expect(paragraph, 'text candidate is collected using an opaque body fallback').toBeDefined();
    expect(paragraph?.background).toMatch(/^rgb\(\d+ \d+ \d+\)$/);

    await page.setContent('<html style="background: rgba(1, 2, 3, .5)"><body style="background: rgba(4, 5, 6, .25)"><p>No opaque surface</p></body></html>');
    await expect(collectContrastCandidates(page)).rejects.toThrow(/page surface is not opaque/);
  });
});

test.describe('dark theme contrast on signed-out routes', () => {
  test('login text and controls measure WCAG AA in both themes', async ({ browser }) => {
    for (const scheme of ['light', 'dark'] as const) {
      const { context, page } = await newContext(browser, scheme);
      await page.goto('/login');
      await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
      const rows = await auditContrast(page, `login-${scheme}`);
      expect(rows.filter((row) => row.kind === 'text' && !row.exempt && row.ratio !== null).length, `${scheme} login measured text`).toBeGreaterThan(0);
      expect(contrastShortfalls(rows), `${scheme} login contrast failures`).toEqual([]);
      await context.close();
    }
  });

});

test.describe('self-hosted Inter Variable', () => {
  test('keeps the first app render behind the primary Inter face', async ({ browser }) => {
    const context = await browser.newContext({ colorScheme: 'light', viewport: { width: 390, height: 844 }, locale: 'en-US' });
    const page = await context.newPage();
    await page.route(/\/assets\/inter-.*\.woff2$/, async (route) => { await new Promise((resolve) => setTimeout(resolve, 400)); await route.continue(); });
    await page.addInitScript(() => {
      const capture = () => {
        const root = document.getElementById('root');
        const state = window as unknown as { __rootFontState?: { available: boolean } };
        if (!state.__rootFontState && root?.childElementCount) {
          state.__rootFontState = {
            available: document.fonts.check('400 16px "Inter Variable"', 'The quick brown fox'),
          };
        }
      };
      const tick = () => {
        capture();
        if (!(window as unknown as { __rootFontState?: unknown }).__rootFontState) requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    });
    await page.goto('/login');
    await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    const state = await page.evaluate(() => (window as unknown as { __rootFontState?: { available: boolean } }).__rootFontState);
    expect(state, 'the first root render is observed').toBeDefined();
    expect(state!.available, 'Inter is available when the app first renders').toBe(true);
    await context.close();
  });

  test('serves both woff2 subsets, covers Czech glyphs, and ships the OFL license', async ({ browser }) => {
    const CZECH_SAMPLE = 'ěščřžýáíéůúďťň';
    const context = await browser.newContext({ colorScheme: 'light', viewport: { width: 390, height: 844 }, locale: 'en-US' });
    const page = await context.newPage();
    const fontResponses: Array<{ url: string; status: number; contentType: string | null }> = [];
    page.on('response', (response) => {
      if (/\.woff2(?:$|\?)/.test(new URL(response.url()).pathname)) {
        fontResponses.push({ url: response.url(), status: response.status(), contentType: response.headers()['content-type'] ?? null });
      }
    });
    await page.goto('/login');
    await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();

    // Render Czech text in Inter, then require the browser to load every face whose unicode-range covers it.
    const result = await page.evaluate(async (sample) => {
      const probe = document.createElement('span');
      probe.lang = 'cs';
      probe.style.fontFamily = '"Inter Variable"';
      probe.style.fontSize = '16px';
      probe.style.fontWeight = '600';
      probe.textContent = sample;
      document.body.append(probe);
      await document.fonts.load('600 16px "Inter Variable"', sample);
      await document.fonts.ready;

      const parseRanges = (value: string): Array<[number, number]> => value.split(',').flatMap((part) => {
        const match = part.trim().toUpperCase().match(/^U\+([0-9A-F?]+)(?:-([0-9A-F?]+))?$/);
        if (!match) return [];
        // A wildcard such as U+01?? spans U+0100–U+01FF: '?' is 0 for the lower bound and F for the upper.
        const hex = (token: string, wildcard: '0' | 'F') => Number.parseInt(token.replaceAll('?', wildcard), 16);
        const start = hex(match[1], '0');
        const end = hex(match[2] ?? match[1], 'F');
        if (Number.isNaN(start) || Number.isNaN(end)) return [];
        return [[start, end] as [number, number]];
      });
      const faces = [...document.fonts]
        .filter((face) => face.family.replaceAll('"', '') === 'Inter Variable')
        .map((face) => ({ status: face.status, weight: face.weight, unicodeRange: face.unicodeRange, ranges: parseRanges(face.unicodeRange) }));
      const loaded = faces.filter((face) => face.status === 'loaded');
      const missing = [...sample]
        .filter((character) => {
          const codePoint = character.codePointAt(0)!;
          return !loaded.some((face) => face.ranges.some(([start, end]) => codePoint >= start && codePoint <= end));
        });
      const heading = document.querySelector('h1');
      return {
        bodyFamily: getComputedStyle(document.body).fontFamily,
        headingFamily: heading ? getComputedStyle(heading).fontFamily : '',
        headingWeight: heading ? getComputedStyle(heading).fontWeight : '',
        czechAvailable: document.fonts.check('600 16px "Inter Variable"', sample),
        loaded,
        missing,
      };
    }, CZECH_SAMPLE);

    expect(result.bodyFamily, 'body uses Inter Variable before the system fallback').toContain('Inter Variable');
    expect(result.headingFamily, 'headings use the same Inter Variable family').toContain('Inter Variable');
    // Page titles share the signed-in semibold scale; 600 is a variable-axis weight, not the regular face.
    expect(result.headingWeight, 'page titles render at the shared semibold weight').toBe('600');
    expect(result.czechAvailable, 'a loaded Inter face reports readiness for the Czech sample').toBe(true);
    expect(result.missing, `loaded Inter unicode ranges cover every glyph in ${CZECH_SAMPLE}`).toEqual([]);
    expect(result.loaded.length, 'both Latin and Latin-ext faces are loaded').toBeGreaterThanOrEqual(2);

    const origin = new URL(page.url()).origin;
    for (const pattern of [/\/assets\/inter-latin-wght-normal-[^/]+\.woff2$/, /\/assets\/inter-latin-ext-wght-normal-[^/]+\.woff2$/]) {
      const response = fontResponses.find((candidate) => pattern.test(new URL(candidate.url).pathname));
      expect(response, `the browser fetched ${pattern}`).toBeDefined();
      expect(new URL(response!.url).origin, 'font is same-origin, never a CDN').toBe(origin);
      expect(response!.status, 'font request succeeds').toBe(200);
      expect(response!.contentType, 'font is served with its real MIME type').toBe('font/woff2');
    }
    for (const response of fontResponses) {
      expect(new URL(response.url).origin, 'font is same-origin, never a CDN').toBe(origin);
      expect(response.url, 'font comes from the hashed asset bundle').toContain('/assets/');
      expect(response.status, 'font request succeeds').toBe(200);
      expect(response.contentType, 'font is served with its real MIME type').toBe('font/woff2');
    }

    const licenseResponse = await page.request.get(new URL('/fonts/INTER-OFL-LICENSE.txt', page.url()).toString());
    expect(licenseResponse.status(), 'the production build ships the Inter OFL license').toBe(200);
    expect(licenseResponse.headers()['content-type'], 'license is served as text').toContain('text/plain');
    const license = await licenseResponse.text();
    expect(license).toContain('The Inter Project Authors');
    expect(license).toContain('SIL OPEN FONT LICENSE Version 1.1');
    await context.close();
  });
});

// Not an appearance proof: this pins the shared target-size predicate (e2e/targetSize.ts) in the gate, which runs this
// file in the `e2e` project. The required journey checks and the opt-in UX audit (e2e/audit/metrics.ts) both measure
// through it. Static markup only; no server state.
test.describe('shared target-size measurement', () => {
  test('counts a stretched row link by the box its overlay really fills, and anything else by itself', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 1600 });
    await page.setContent(`<html><head><style>
      li, .row { position: relative; height: 56px; width: 300px; list-style: none; }
      a { display: inline-block; height: 22px; }
      .stretched::after { content: ''; position: absolute; inset: 0; }
      .partial::after { content: ''; position: absolute; top: 0; right: 0; width: 8px; height: 8px; }
      .no-content::after { content: none; position: absolute; inset: 0; }
      .click-through::after { pointer-events: none; }
      li.small { height: 30px; }
      a.self { position: relative; }
      .transformed { transform: translateX(0); height: 22px; }
      .translated { translate: 0 0; height: 22px; }
      .margin::after { margin: 20px 0; }
      .hidden-overlay::after { visibility: hidden; }
      li.bordered { border: 4px solid; }
      .box { width: 20px; height: 20px; padding: 0; margin: 0; }
      .under::after { z-index: -1; }
      .clipped { clip-path: inset(0); }
      .masked { mask-image: linear-gradient(black, black); height: 22px; }
      .contents { display: contents; position: relative; }
      li.scrolled { overflow-y: scroll; }
      li.scrolled .spacer { height: 200px; }
      table { border-collapse: collapse; } td { height: 56px; padding: 0; }
      .switch { display: inline-block; width: 20px; height: 20px; }
      label { display: flex; min-height: 44px; align-items: center; }
    </style></head><body><ul>
      <li><a class="stretched" href="#a">Stretched row</a></li>
      <li><a class="partial" href="#b">Partial overlay</a></li>
      <li class="small"><a class="stretched" href="#c">Small row</a></li>
      <li><a class="stretched self" href="#d">Self-positioned</a></li>
      <li><a class="no-content" href="#e">Overlay without content</a></li>
      <li><a class="stretched click-through" href="#f">Click-through overlay</a></li>
      <li><div class="transformed"><a class="stretched" href="#g">Transformed wrapper</a></div></li>
      <li><div class="translated"><a class="stretched" href="#j">Translated wrapper</a></div></li>
      <li><a class="stretched margin" href="#k">Overlay inset by margin</a></li>
      <li><a class="stretched hidden-overlay" href="#l">Hidden overlay</a></li>
      <li class="bordered"><a class="stretched" href="#m">Bordered row</a></li>
      <li><a class="stretched under" href="#o">Overlay under the row</a></li>
      <li><a class="stretched clipped" href="#p">Clipped link</a></li>
      <li><div class="masked"><a class="stretched" href="#q">Masked wrapper</a></div></li>
      <li class="scrolled"><a class="stretched" href="#r">Scrolled row</a><div class="spacer"></div></li>
      <li><span class="contents"><a class="stretched" href="#s">Row through a contents wrapper</a></span></li>
    </ul>
    <table><tr><td><a class="stretched" href="#h">Static table cell</a></td></tr></table>
    <div class="row"><table><tr><td><a class="stretched" href="#i">Cell in a positioned row</a></td></tr></table></div>
    <label><button class="switch" role="switch" aria-checked="false" aria-label="Labelled switch"></button> Labelled switch</label>
    <label><input class="box" type="checkbox" aria-label="Agree"> Agree to the <a href="#n">terms</a></label>
    <label><input class="box" type="checkbox" aria-label="First control"><button class="box">y</button></label>
    <button style="width: 20px; height: 20px; padding: 0">x</button></body></html>`);
    await page.locator('li.scrolled').evaluate((row) => { row.scrollTop = 40; });
    const metrics = await measureCapture(page);
    expect(metrics.targetOffenders.map((offender) => offender.name)).toEqual([
      'Partial overlay', 'Small row', 'Self-positioned', 'Overlay without content', 'Click-through overlay', 'Transformed wrapper',
      'Translated wrapper', 'Overlay inset by margin',
    ]);
    expect(metrics.interactiveTargetsUnder44).toBe(17);
    expect(metrics.interactiveTargetsUnder24).toBe(16);
    // measureCapture reports the first eight in-viewport offenders; the predicate itself measures every control.
    const all = await page.locator('a[href],button,input').evaluateAll(measureTargets);
    expect(all.filter((target) => pressedBelow(target, 44)).map((target) => target.ariaLabel || target.text)).toEqual([
      'Partial overlay', 'Small row', 'Self-positioned', 'Overlay without content', 'Click-through overlay', 'Transformed wrapper',
      'Translated wrapper', 'Overlay inset by margin', 'Hidden overlay', 'Overlay under the row', 'Clipped link', 'Masked wrapper',
      'Scrolled row', 'Static table cell', 'terms', 'y', 'x',
    ]);
    // The stretched rows that pass are measured by the box the overlay covers, not by the 22 px text line.
    // A bordered row is pressed in its padding box, which the overlay fills, not its border box; a display: contents
    // wrapper is skipped to the real row. A checkbox is pressed in the label that controls it.
    const pressed = await page.locator('a[href="#a"], a[href="#i"], a[href="#m"], a[href="#s"], input[aria-label="Agree"]').evaluateAll(measureTargets);
    expect(pressed.map((target) => [Math.round(target.pressed.width), Math.round(target.pressed.height)])).toEqual([[300, 56], [300, 56], [300, 56], [300, 56], [390 - 16, 44]]);
  });
});
