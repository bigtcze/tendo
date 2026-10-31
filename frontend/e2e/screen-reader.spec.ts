import { expect, test, type Page } from '@playwright/test';
import { appendFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { en } from '../src/i18n/en';
import { realKey } from './screen-reader/orca';
import { cs } from '../src/i18n/cs';

const dir = process.env.E2E_SR_AUDIT_DIR ? resolve(process.env.E2E_SR_AUDIT_DIR) : '';
const debugPath = process.env.E2E_SR_DEBUG_FILE ? resolve(process.env.E2E_SR_DEBUG_FILE) : '';
const setupToken = process.env.E2E_SETUP_TOKEN!;
const expectedFor = (locale: 'en' | 'cs') => locale === 'en' ? en : cs;
const transcript: Array<{ step: string; locale: string; action: string; expected: string[]; spoken: string[]; focused: { tag: string; id: string | null; name: string | null } | null; focusExpected: string; focusMatches: boolean; expectedLiveText?: string; liveTextMatches?: boolean; speechBytes: number; speechCountBefore: number; speechCountAfter: number; actionCorrelated: boolean; result: 'pass' | 'finding' | 'fail' }> = [];
const ownerLogin = 'sr_owner';
const householdName = 'Screen reader household';
const ownerPassword = process.env.E2E_OWNER_PASSWORD ?? '';
const secretValues = [setupToken, ownerPassword];
const redactText = (value: string) => secretValues.reduce((text, secret) => secret ? text.split(secret).join('[redacted]') : text, value)
  .replace(/https?:\/\/[^\s'"<>]+/g, '[redacted-url]')
  .replace(/(?:set-cookie|cookie):\s*[^\r\n]*/gi, '[redacted-cookie]');
function speechSince(offset: number) {
  return readFileSync(debugPath).subarray(offset).toString('utf8').split(/\r?\n/)
    .filter((line) => line.includes('SPEECH OUTPUT:'))
    .map((line) => line.match(/SPEECH OUTPUT:\s*'((?:\\.|[^'])*)'/)?.[1]?.replace(/\\'/g, "'").trim() ?? '')
    .filter(Boolean);
}
function sanitizeCatalogText(value: string) {
  return value;
}
const QUIET_MS = 1_000;
const SPEECH_WAIT_MS = 8_000;
const TEST_CLOCK = process.env.E2E_TEST_CLOCK_NOW ?? '2026-05-14T08:00:00Z';
const MUTATION_MODE = process.env.TENDO_SR_MUTATION ?? '';

function assertRedaction() {
  const normal = 'Setup code entry required. Screen reader household sr_owner.';
  const redacted = redactText(`speech ${setupToken} ${ownerPassword} ${normal}`);
  if (redacted.includes(setupToken) || redacted.includes(ownerPassword) || !redacted.includes(normal)) throw new Error('Transcript redaction self-check failed');
}

function testFailure(message: string): never {
  throw new Error(`[mutation:${MUTATION_MODE || 'orc-disabled'}] ${message}`);
}
async function assertOrcaStarted() {
  if (process.env.E2E_SR_DISABLE_ORCA === '1') testFailure('Orca is required but was explicitly disabled');
  const initial = readFileSync(debugPath, 'utf8');
  if (!initial.includes('Orca version') || !initial.includes('SPEECH OUTPUT:')) testFailure('Orca failed startup/speech self-check');
}

async function assertChromiumRegisteredWithAtSpi() {
  const deadline = Date.now() + 15_000;
  const registration = /object:children-changed:add for \[desktop frame: '[^']+'\] in \(\d+, \d+, \[application: 'Google Chrome for (?:Testing|Test)/;
  while (Date.now() < deadline) {
    if (registration.test(readFileSync(debugPath, 'utf8'))) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  testFailure('Chromium did not register with AT-SPI within 15 seconds after first page load');
}

function activateBrowserWindow() {
  const windows = execFileSync('xdotool', ['search', '--onlyvisible', '--name', 'Google Chrome for Testing'], { encoding: 'utf8' }).trim().split(/\s+/).filter(Boolean);
  for (const id of windows) {
    const pid = execFileSync('xdotool', ['getwindowpid', id], { encoding: 'utf8' }).trim();
    const owned = execFileSync('xdotool', ['search', '--onlyvisible', '--pid', pid], { encoding: 'utf8' }).trim().split(/\s+/);
    if (!owned.includes(id)) continue;
    execFileSync('xdotool', ['windowactivate', '--sync', id]);
    execFileSync('xdotool', ['windowfocus', '--sync', id]);
    return;
  }
  throw new Error('No visible Playwright Chromium window owned by its browser process');
}

// Waits for Orca to stop speaking. With afterCount, first waits (bounded) until speech beyond that
// count appears, so a slow first utterance after an action is not mistaken for silence.
async function waitForSpeechQuiet(page: Page, timeout = SPEECH_WAIT_MS, afterCount?: number) {
  const deadline = Date.now() + timeout;
  let transcript = readFileSync(debugPath);
  if (!transcript.includes('Orca version')) testFailure('Orca is not running while waiting for speech quiet');
  let speechCount = (transcript.toString('utf8').match(/SPEECH OUTPUT:/g) ?? []).length;
  const startCount = speechCount;
  let quietSince = Date.now();
  while (Date.now() < deadline) {
    await page.waitForTimeout(100);
    const next = readFileSync(debugPath);
    const nextCount = (next.toString('utf8').match(/SPEECH OUTPUT:/g) ?? []).length;
    if (nextCount !== speechCount) {
      transcript = next;
      speechCount = nextCount;
      quietSince = Date.now();
    }
    const spokeAfterAction = afterCount === undefined || speechCount > afterCount;
    if (spokeAfterAction && Date.now() - quietSince >= QUIET_MS) return { bytes: transcript.length, speechCount, startCount };
  }
  if (afterCount !== undefined && speechCount <= afterCount) return { bytes: transcript.length, speechCount, startCount };
  throw new Error(`Orca speech did not become quiet within ${timeout}ms`);
}

async function evidence(
  page: Page,
  locale: 'en' | 'cs',
  step: string,
  action: string,
  key: string,
  expected: string[],
  focusedSelector: string,
  expectedLiveText?: string,
) {
  const quiet = await waitForSpeechQuiet(page);
  const offset = quiet.bytes;
  const beforeSpeechCount = quiet.speechCount;
  await realKey(page, key);
  const settled = await waitForSpeechQuiet(page, SPEECH_WAIT_MS, beforeSpeechCount);
  const spoken = speechSince(offset);
  const focused = await page.evaluate(() => {
    const active = document.activeElement;
    return active instanceof HTMLElement ? {
      tag: active.tagName.toLowerCase(),
      id: active.id || null,
      name: active.getAttribute('aria-label') || active.textContent?.trim().slice(0, 100) || null,
    } : null;
  });
  const focusMatches = await page.locator(focusedSelector).evaluate((node) => node === document.activeElement);
  const actionCorrelated = settled.speechCount > beforeSpeechCount && spoken.length > 0;
  const liveTextMatches = expectedLiveText
    ? await page.getByRole('status').filter({ hasText: expectedLiveText }).count() > 0
    : undefined;
  const normalized = spoken.join(' ').toLocaleLowerCase(locale === 'cs' ? 'cs-CZ' : 'en-US');
  const pass = expected.some((phrase) => normalized.includes(phrase.toLocaleLowerCase(locale === 'cs' ? 'cs-CZ' : 'en-US')));
  const stepPass = pass && focusMatches && liveTextMatches !== false && actionCorrelated;
  const finding = expectedLiveText !== undefined && liveTextMatches === true && !pass;
  transcript.push({
    step, locale, action, expected: expected.map(sanitizeCatalogText), spoken: spoken.map(redactText),
    focused: focused ? { ...focused, name: focused.name ? redactText(focused.name) : null } : null,
    focusExpected: focusedSelector, focusMatches, expectedLiveText: expectedLiveText ? sanitizeCatalogText(expectedLiveText) : undefined, liveTextMatches, speechBytes: settled.bytes - offset, speechCountBefore: beforeSpeechCount, speechCountAfter: settled.speechCount, actionCorrelated,
    result: stepPass ? 'pass' : finding ? 'finding' : 'fail',
    ...(MUTATION_MODE && stepPass ? { _mutationObservedPass: true } : {}),
  });
  if (!focusMatches) testFailure(`${step}: DOM focus did not match ${focusedSelector}; actual=${JSON.stringify(focused)}`);
  if (expectedLiveText && !liveTextMatches) testFailure(`${step}: expected live-region text was absent`);
  if (!actionCorrelated) testFailure(`${step}: Orca produced no speech after the action (before=${beforeSpeechCount}, after=${settled.speechCount})`);
  if (!pass && !finding) testFailure(`${step}: expected ${expected.join(' | ')} after action in Orca speech: ${spoken.join(' / ')}`);
}

async function clickContent(page: Page) {
  activateBrowserWindow();
  const point = await page.evaluate(() => {
    const heading = document.querySelector('h1');
    const rect = heading?.getBoundingClientRect();
    const chromeX = window.outerWidth - window.innerWidth;
    const chromeY = window.outerHeight - window.innerHeight;
    return {
      screenX: window.screenX,
      screenY: window.screenY,
      innerWidth: window.innerWidth,
      innerHeight: window.innerHeight,
      outerWidth: window.outerWidth,
      outerHeight: window.outerHeight,
      chromeX,
      chromeY,
      x: Math.round(window.screenX + chromeX + (rect?.left ?? 8) + 4),
      y: Math.round(window.screenY + chromeY + (rect?.top ?? 8) + 4),
    };
  });
  const before = readFileSync(debugPath).length;
  execFileSync('xdotool', ['mousemove', '--sync', String(point.x), String(point.y), 'click', '1']);
  await page.waitForTimeout(250);
  const activeWindow = execFileSync('xdotool', ['getactivewindow'], { encoding: 'utf8' }).trim();
  const activePid = execFileSync('xdotool', ['getwindowpid', activeWindow], { encoding: 'utf8' }).trim();
  const browserWindow = execFileSync('xdotool', ['search', '--onlyvisible', '--pid', activePid], { encoding: 'utf8' }).trim().split(/\s+/);
  if (!browserWindow.includes(activeWindow)) throw new Error(`Browser window lost activation after content click: ${activeWindow}`);
  const spoken = speechSince(before).join(' ').toLowerCase();
  if (/address and search bar|omnibox|bookmark this tab/.test(spoken)) throw new Error(`Content click reached browser chrome: ${spoken}`);
  const focusedControl = await page.evaluate(() => {
    const active = document.activeElement;
    return active instanceof HTMLElement && active.matches('input,button,a,select,textarea,[tabindex]:not([tabindex="-1"])')
      ? active.outerHTML : null;
  });
  if (focusedControl) throw new Error(`Content click unexpectedly focused a control: ${focusedControl}`);
}

async function focusControl(page: Page, selector: string) {
  await page.locator(selector).focus();
  await expect(page.locator(selector)).toBeFocused();
  await waitForSpeechQuiet(page);
}

test.describe.configure({ mode: 'serial' });
test.setTimeout(600_000);

test('Orca speaks localized screen-reader evidence', async ({ page }) => {
  if (!dir || !debugPath) throw new Error('E2E_SR_AUDIT_DIR and E2E_SR_DEBUG_FILE are required for the screen-reader audit');
  mkdirSync(dir, { recursive: true });
  assertRedaction();
  await page.route('**/api/v1/auth/setup', async (route) => {
    if (route.request().method() === 'PUT') {
      await route.continue({ headers: { ...route.request().headers(), 'x-tendo-setup-token': setupToken } });
    } else await route.continue();
  });
  if (MUTATION_MODE === 'live-region-role') await page.addInitScript(() => {
    const observer = new MutationObserver(() => document.querySelectorAll('[role="status"]').forEach((node) => node.removeAttribute('role')));
    observer.observe(document, { subtree: true, childList: true, attributes: true });
  });
  if (!debugPath || !readFileSync(debugPath, 'utf8').length) throw new Error('Orca debug transcript is missing or empty');
  await assertOrcaStarted();
  const version = (command: string, args: string[], cwd?: string) => { try { return execFileSync(command, args, { encoding: 'utf8', timeout: 3000, ...(cwd ? { cwd } : {}) }).trim().split('\n')[0] ?? 'unknown'; } catch { return 'unavailable'; } };
  const packageVersion = (command: string, args: string[]) => { try { return execFileSync(command, args, { encoding: 'utf8', timeout: 3000 }).trim(); } catch { return 'unavailable'; } };
  const packageVersionOrFallback = (primary: string, fallback: string) => packageVersion('dpkg-query', ['-W', '-f=${Version}', primary]) || packageVersion('dpkg-query', ['-W', '-f=${Version}', fallback]);
  const start = new Date().toISOString();
  let created = false;
  for (const locale of ['en'] as const) {
    const catalog = expectedFor(locale);
    if (!created) {
      await page.goto('/');
      await assertChromiumRegisteredWithAtSpi();
      await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
      await waitForSpeechQuiet(page);
      await clickContent(page);
      const setupBounds = await page.locator('#setup-code').boundingBox();
      if (!setupBounds) throw new Error('Setup-code control has no visible bounds');
      const setupPoint = await page.evaluate(({ x, y }) => {
        const chromeX = window.outerWidth - window.innerWidth;
        const chromeY = window.outerHeight - window.innerHeight;
        return { x: Math.round(window.screenX + chromeX + x), y: Math.round(window.screenY + chromeY + y) };
      }, { x: setupBounds.x + setupBounds.width / 2, y: setupBounds.y + setupBounds.height / 2 });
      execFileSync('xdotool', ['mousemove', '--sync', String(setupPoint.x), String(setupPoint.y), 'click', '1']);
      await expect(page.locator('#setup-code')).toBeFocused();
      await waitForSpeechQuiet(page);
      await evidence(page, 'en', 'onboarding.setup-code', 'Tab from setup-code control', 'Tab', [en['onboarding.householdName']], '#household-name');
      // Continue with real keys before any programmatic fill: a burst of fill() focus/caret events can leave
      // Orca's locus of focus on a different field than the DOM, which silences the next Tab announcement.
      await evidence(page, 'en', 'onboarding.household-to-login', 'Tab from household name to Login', 'Tab', [en['onboarding.login']], '#onboarding-login');
      await page.locator('#setup-code').fill(setupToken);
      await page.getByLabel(en['onboarding.householdName']).fill(householdName);
      await page.locator('#onboarding-login').fill(ownerLogin);
      await page.locator('#onboarding-password').fill(process.env.E2E_OWNER_PASSWORD!);
      await page.getByRole('button', { name: en['onboarding.submit'] }).click();
      await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
      await waitForSpeechQuiet(page);
      created = true;
    }
    await page.evaluate((value) => localStorage.setItem('tendo.locale', value), locale);
    await page.reload();
    await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
    await waitForSpeechQuiet(page);
    await clickContent(page);
    await focusControl(page, 'nav a[href="/"]');
    await evidence(page, locale, 'home.navigation', 'Tab to All items navigation', 'Tab', [catalog['nav.allItems']], 'nav a[href="/items"]');
    await evidence(page, locale, 'home.navigation-activate', 'Activate All items navigation', 'Return', [catalog['allItems.title']], 'h1');
    await expect(page.getByRole('heading', { level: 1, name: catalog['allItems.title'] })).toBeVisible();
    await focusControl(page, '#all-items-search');
    await evidence(page, locale, 'all-items.search', 'Tab from search field to Search button', 'Tab', [catalog['allItems.searchButton']], 'button[type="submit"]');
    await focusControl(page, 'nav a[href="/people"]');
    await evidence(page, locale, 'people.navigation', 'Activate people navigation; focus moves to the page heading', 'Return', [catalog['subjects.title']], 'h1');
    await expect(page.getByRole('heading', { level: 1, name: catalog['subjects.title'] })).toBeVisible();
    await focusControl(page, 'nav a[href="/members"]');
    await evidence(page, locale, 'members.navigation', 'Activate members navigation; focus moves to the page heading', 'Return', [catalog['members.title']], 'h1');
    await expect(page.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
    await focusControl(page, 'nav a[href="/account"]');
    await evidence(page, locale, 'account.navigation', 'Activate account navigation; focus moves to the page heading', 'Return', [catalog['account.title']], 'h1');
    await expect(page.getByRole('heading', { level: 1, name: catalog['account.title'] })).toBeVisible();
    await page.goto('/');
  }
  const manifest = {
    commit: process.env.GIT_COMMIT ?? 'unknown', dirty: process.env.GIT_DIRTY === 'true',
    versions: { orca: version('orca', ['--version']), atSpi: packageVersionOrFallback('libatspi2.0-0t64', 'libatspi2.0-0'), speechDispatcher: version('speech-dispatcher', ['--version']), espeakNg: packageVersionOrFallback('libespeak-ng1', 'espeak-ng'), chromium: version(version('node', ['-p', "require('@playwright/test').chromium.executablePath()"], process.cwd()), ['--version']), playwright: version('node', ['-p', "require('./node_modules/@playwright/test/package.json').version"], process.cwd()) },
    command: process.env.TENDO_SR_COMMAND ?? 'TENDO_SR_AUDIT_DIR=... bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh',
    orcaSettings: 'Orca defaults in a private -u directory; orca-customizations.py only switches the debug file to line buffering; audio to file-backed ALSA',
    display: { size: '1280x900', display: process.env.DISPLAY },
    testClock: TEST_CLOCK, locale: 'en+cs', startedAt: start, finishedAt: new Date().toISOString(),
  };
  writeFileSync(resolve(dir, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
  writeFileSync(resolve(dir, 'transcript.json'), `${JSON.stringify(transcript, null, 2)}\n`);
  writeFileSync(resolve(dir, 'transcript.md'), `# Orca screen-reader evidence\n\n${transcript.map((entry) => `## ${entry.step} (${entry.locale})\n\n- action: ${entry.action}\n- expected: ${entry.expected.join(' OR ')}\n- spoken: ${entry.spoken.join(' | ') || '(none)'}\n- focused: ${JSON.stringify(entry.focused)} (matches ${entry.focusExpected}: ${entry.focusMatches})\n- result: ${entry.result}`).join('\n\n')}\n`);
  appendFileSync(resolve(dir, 'transcript.md'), `\n\nOnly the recorded steps above are asserted; this is not user testing or accessibility certification.\n`);
  expect(transcript.length).toBeGreaterThan(0);
  expect(transcript.every((step) => step.result !== 'fail'), 'unresolved screen-reader assertions').toBe(true);
  expect(transcript.filter((step) => step.result === 'pass')).toHaveLength(transcript.length);
});
