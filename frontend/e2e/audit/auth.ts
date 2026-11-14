import { expect, type Browser, type Locator, type Page } from '@playwright/test';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { expectNoAxeViolations } from '../a11y';
import { applySavedDarkPreference, applySystemAppearance } from './appearance';

const widths = [320, 390, 768, 1024, 1440] as const;
const heightFor = (width: number) => width <= 320 ? 640 : width < 768 ? 844 : width < 1440 ? 1024 : 900;
type Capture = (page: Page, path: string, fullPage?: boolean, mask?: Locator[]) => Promise<void>;

async function shot(page: Page, dir: string, name: string, capture: Capture, width: number, mask: Locator[] = []) {
  const size = page.viewportSize();
  expect(size?.width, `${name} configured viewport`).toBe(width);
  expect(await page.evaluate(() => window.innerWidth), `${name} actual viewport`).toBe(width);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${name} has no horizontal overflow`).toBe(true);
  await capture(page, join(dir, name), false, mask);
}

/**
 * Viewport capture of the onboarding recovery area: the error alert, the timezone select and the submit button must all be
 * visible in the same frame. Scrolls once, asserts, then screenshots with page.screenshot so no measurement or focus reset
 * runs between the scroll and the image.
 */
async function captureRecoveryArea(page: Page, path: string, timezoneLabel: string, submitLabel: string) {
  const alert = page.getByRole('alert');
  const timezone = page.getByLabel(timezoneLabel, { exact: true });
  const submit = page.getByRole('button', { name: submitLabel, exact: true });
  await alert.scrollIntoViewIfNeeded();
  await expect(alert, 'error alert in viewport').toBeInViewport();
  await expect(timezone, 'timezone visible in recovery frame').toBeVisible();
  await expect(submit, 'submit visible in recovery frame').toBeVisible();
  const bounds = await page.evaluate(() => {
    const at = (selector: string) => {
      const element = document.querySelector<HTMLElement>(selector);
      return element ? element.getBoundingClientRect().bottom : Infinity;
    };
    return { alertBottom: at('[role="alert"]'), timezoneBottom: at('#onboarding-timezone'), submitBottom: at('button[type="submit"]') };
  });
  const viewportHeight = page.viewportSize()?.height ?? 0;
  for (const [name, bottom] of Object.entries(bounds)) {
    expect(bottom, `${name} starts inside the captured viewport`).toBeLessThan(viewportHeight);
    expect(bottom, `${name} has a finite position`).toBeGreaterThan(0);
  }
  await expect(timezone, 'timezone in viewport').toBeInViewport();
  await expect(submit, 'submit in viewport').toBeInViewport();
  await page.screenshot({ path, fullPage: false });
}

async function setLocale(page: Page, locale: 'en' | 'cs') {
  await page.evaluate((value) => localStorage.setItem('tendo.locale', value), locale);
  await page.reload();
}

async function focusAppearanceChoiceByKeyboard(page: Page, name: string) {
  const appearance = page.getByRole('main').getByRole('group', { name: en['account.appearance.title'], exact: true });
  const choice = appearance.getByRole('button', { name, exact: true });
  for (let tab = 0; tab < 30 && !(await choice.evaluate((element) => element === document.activeElement)); tab += 1) {
    await page.keyboard.press('Tab');
  }
  await expect(choice, `${name} appearance choice receives keyboard focus`).toBeFocused();
  await expect(choice).toHaveCSS('outline-style', 'solid');
  return choice;
}

/** Auth/first-run capture scenarios. Synthetic password/setup material is only typed into masked fields, never captured. */
export async function captureAuth(page: Page, browser: Browser, dir: string, capture: Capture) {
  for (const width of widths) {
    const setupContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
    const setupPage = await setupContext.newPage();
    await setupPage.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
    await setupPage.route('**/api/v1/auth/setup', route => route.request().method() === 'GET'
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: true }) })
      : route.fallback());
    await setupPage.goto('/');
    await expect(setupPage.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
    await shot(setupPage, dir, `auth-onboarding-en-${width}.png`, capture, width);
    await setupContext.close();
    if (width === 390 || width === 1440) {
      const csContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
      const csPage = await csContext.newPage();
      await csPage.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
      await csPage.route('**/api/v1/auth/setup', route => route.request().method() === 'GET'
        ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: true }) })
        : route.fallback());
      await csPage.goto('/');
      await setLocale(csPage, 'cs');
      await expect(csPage.getByRole('heading', { level: 1, name: cs['onboarding.title'] })).toBeVisible();
      await shot(csPage, dir, `auth-onboarding-cs-${width}.png`, capture, width);
      await csContext.close();
    }
  }
  const validationContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const validationPage = await validationContext.newPage();
  await validationPage.route('**/api/v1/auth/setup', route => route.request().method() === 'GET'
    ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: true }) })
    : route.fallback());
  await validationPage.goto('/');
  await validationPage.getByRole('button', { name: en['onboarding.submit'], exact: true }).click();
  await expect(validationPage.getByRole('alert')).toBeVisible();
  await expectNoAxeViolations(validationPage, 'auth-onboarding-validation');
  await capture(validationPage, join(dir, 'auth-onboarding-validation-en-390.png'), true);
  await captureRecoveryArea(validationPage, join(dir, 'auth-onboarding-validation-area-en-390.png'), en['onboarding.timezone'], en['onboarding.submit']);
  await validationContext.close();

  const setupErrorContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const setupError = await setupErrorContext.newPage();
  await setupError.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
  await setupError.route('**/api/v1/auth/setup', route => route.request().method() === 'GET'
    ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: true }) })
    : route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' }) }));
  await setupError.goto('/');
  await setupError.getByLabel(en['onboarding.setupCode'], { exact: true }).fill('auth-audit-code');
  await setupError.getByLabel(en['onboarding.householdName'], { exact: true }).fill('Audit home');
  await setupError.getByLabel(en['onboarding.login'], { exact: true }).fill('audit_owner');
  await setupError.getByLabel(en['onboarding.password'], { exact: true }).fill('synthetic-password-never-captured');
  await setupError.getByRole('button', { name: en['onboarding.submit'], exact: true }).click();
  await expect(setupError.getByRole('alert')).toBeVisible();
  await capture(setupError, join(dir, 'auth-onboarding-server-error-en-390.png'), true);
  await captureRecoveryArea(setupError, join(dir, 'auth-onboarding-server-error-area-en-390.png'), en['onboarding.timezone'], en['onboarding.submit']);
  await setupErrorContext.close();

  for (const width of widths) {
    const context = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
    const signedOut = await context.newPage();
    await signedOut.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
    await signedOut.route('**/api/v1/auth/setup', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: false }) }));
    await signedOut.route('**/api/v1/auth/oidc', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ enabled: true, displayName: 'OIDC Smoke Provider' }) }));
    await signedOut.goto('/login');
    await expect(signedOut.getByRole('heading', { level: 1, name: en['login.welcome'] })).toBeVisible();
    if (width >= 768) {
      await expect(signedOut.locator('main section')).toHaveCSS('max-width', '448px');
      // 448 px card − 2 × 28 px padding − 2 px hairline border.
      await expect(signedOut.getByRole('form', { name: en['login.heading'] }).locator('button[type="submit"]')).toHaveCSS('width', '390px');
    }
    await expectNoAxeViolations(signedOut, `auth-login-${width}`);
    await shot(signedOut, dir, `auth-login-en-${width}.png`, capture, width);
    if (width === 390 || width === 1440) {
      await applySavedDarkPreference(signedOut);
      await expect(signedOut.getByRole('heading', { level: 1, name: en['login.welcome'] })).toBeVisible();
      await shot(signedOut, dir, `auth-login-dark-en-${width}.png`, capture, width);
      await applySystemAppearance(signedOut);
      await setLocale(signedOut, 'cs');
      await expect(signedOut.getByRole('heading', { level: 1, name: cs['login.welcome'] })).toBeVisible();
      await shot(signedOut, dir, `auth-login-cs-${width}.png`, capture, width);
      await signedOut.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
      await signedOut.reload();
    }
    await signedOut.getByLabel(en['login.login'], { exact: true }).fill('audit_missing_user');
    await signedOut.getByLabel(en['login.password'], { exact: true }).fill('wrong-synthetic-password');
    await signedOut.getByRole('button', { name: en['login.submit'], exact: true }).click();
    await expect(signedOut.getByRole('alert')).toContainText('username or password');
    await expect(signedOut.getByLabel(en['login.login'], { exact: true })).not.toHaveAttribute('aria-invalid', 'true');
    await expect(signedOut.getByLabel(en['login.password'], { exact: true })).not.toHaveAttribute('aria-invalid', 'true');
    await shot(signedOut, dir, `auth-login-error-en-${width}.png`, capture, width);
    if (width === 390) {
      await signedOut.route('**/api/v1/session', route => route.request().method() === 'POST'
        ? route.fulfill({ status: 429, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Limited', status: 429 }) })
        : route.fallback());
      await signedOut.getByLabel(en['login.password'], { exact: true }).fill('wrong-synthetic-password');
      await signedOut.getByRole('button', { name: en['login.submit'], exact: true }).click();
      await expect(signedOut.getByRole('alert')).toContainText(en['login.error.rateLimited']);
      await shot(signedOut, dir, 'auth-login-rate-limited-en-390.png', capture, 390);
    }
    await context.close();
  }

  const sessionEnded = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const ended = await sessionEnded.newPage();
  await ended.route('**/api/v1/session', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ userId: '0198a2f0-0000-7000-8000-000000000001', login: 'audit_owner', defaultHouseholdId: '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61', expiresAt: '2030-01-01T00:00:00Z' }) }));
  await ended.route('**/api/v1/households/*', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
  await ended.route('**/api/v1/auth/setup', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: false }) }));
  await ended.goto('/');
  await expect(ended.getByRole('heading', { level: 1, name: en['login.welcome'] })).toBeVisible();
  await expect(ended.getByRole('status')).toContainText(en['login.sessionEnded']);
  await shot(ended, dir, 'auth-login-session-ended-en-390.png', capture, 390);
  await sessionEnded.close();

  const oidcContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const oidc = await oidcContext.newPage();
  await oidc.route('**/api/v1/auth/oidc', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ enabled: true, displayName: 'OIDC Smoke Provider' }) }));
  await oidc.goto('/login');
  await expect(oidc.getByRole('button', { name: en['login.oidc.submit'].replace('{provider}', 'OIDC Smoke Provider'), exact: true })).toBeVisible();
  await shot(oidc, dir, 'auth-login-oidc-available-en-390.png', capture, 390);
  await oidc.unroute('**/api/v1/auth/oidc');
  let providerChecks = 0;
  await oidc.route('**/api/v1/auth/oidc', route => {
    providerChecks += 1;
    return route.fulfill({ status: providerChecks === 1 ? 503 : 200, contentType: 'application/json', body: JSON.stringify({ enabled: true, displayName: 'OIDC Smoke Provider' }) });
  });
  await oidc.reload();
  await expect(oidc.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
  await shot(oidc, dir, 'auth-login-oidc-provider-error-en-390.png', capture, 390);
  await oidc.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
  await expect(oidc.getByRole('button', { name: en['login.oidc.submit'].replace('{provider}', 'OIDC Smoke Provider'), exact: true })).toBeVisible();
  const oidcOutcomePage = await oidcContext.newPage();
  await oidcOutcomePage.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
  await oidcOutcomePage.route('**/api/v1/auth/setup', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: false }) }));
  // A provider callback always arrives as a full document load. Playwright's goto to the same path with a
  // different fragment is a same-document navigation, so the app would never re-read the fragment: pass
  // through about:blank to give every outcome a fresh document, exactly like the real redirect.
  const openLoginFragment = async (hash: string) => {
    await oidcOutcomePage.goto('about:blank');
    await oidcOutcomePage.goto(`/login#${hash}`);
  };
  await openLoginFragment('oidcError=cancelled');
  await expect(oidcOutcomePage.getByRole('status')).toContainText(en['oidc.error.cancelled']);
  expect(new URL(oidcOutcomePage.url()).hash, 'OIDC callback fragment is scrubbed').toBe('');
  await shot(oidcOutcomePage, dir, 'auth-login-oidc-outcome-en-390.png', capture, 390);

  // identity_conflict: the callback refuses an identity whose verified address belongs to an account
  // with a different subject. Copy is asserted from the catalogs so audit and product stay in sync.
  await openLoginFragment('oidcError=identity_conflict');
  await expect(oidcOutcomePage.getByRole('status')).toHaveText(en['oidc.error.identity_conflict']);
  expect(new URL(oidcOutcomePage.url()).hash, 'conflict callback fragment is scrubbed').toBe('');
  await expectNoAxeViolations(oidcOutcomePage, 'auth-login-oidc-conflict-en-390');
  await shot(oidcOutcomePage, dir, 'auth-login-oidc-conflict-en-390.png', capture, 390);

  await oidcOutcomePage.setViewportSize({ width: 1440, height: heightFor(1440) });
  await expect(oidcOutcomePage.getByRole('status')).toHaveText(en['oidc.error.identity_conflict']);
  await expectNoAxeViolations(oidcOutcomePage, 'auth-login-oidc-conflict-en-1440');
  await shot(oidcOutcomePage, dir, 'auth-login-oidc-conflict-en-1440.png', capture, 1440);

  await oidcOutcomePage.setViewportSize({ width: 390, height: heightFor(390) });
  await applySavedDarkPreference(oidcOutcomePage);
  await openLoginFragment('oidcError=identity_conflict');
  await expect(oidcOutcomePage.getByRole('status')).toHaveText(en['oidc.error.identity_conflict']);
  await expectNoAxeViolations(oidcOutcomePage, 'auth-login-oidc-conflict-dark-en-390');
  await shot(oidcOutcomePage, dir, 'auth-login-oidc-conflict-dark-en-390.png', capture, 390);
  await applySystemAppearance(oidcOutcomePage);

  await oidcOutcomePage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await openLoginFragment('oidcError=identity_conflict');
  await expect(oidcOutcomePage.getByRole('status')).toHaveText(cs['oidc.error.identity_conflict']);
  await expectNoAxeViolations(oidcOutcomePage, 'auth-login-oidc-conflict-cs-390');
  await shot(oidcOutcomePage, dir, 'auth-login-oidc-conflict-cs-390.png', capture, 390);
  await oidcContext.close();

  for (const width of [390, 1440] as const) {
    const accountContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
    const accountPage = await accountContext.newPage();
    await accountPage.route('**/api/v1/session', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ userId: '0198a2f0-0000-7000-8000-000000000001', login: 'audit_owner', expiresAt: '2030-01-01T00:00:00Z' }) }));
    await accountPage.route('**/api/v1/auth/setup', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: false }) }));
    await accountPage.goto('/account');
    const page = accountPage;
    await expect(page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
    await expect(page.getByRole('main').locator('section[aria-labelledby="account-identity-heading"]')).toContainText(en['account.signedInAs'].replace('{login}', 'audit_owner'));
    await expect(page.getByRole('main').locator('input[type="password"]')).toHaveCount(0);
    await expect(page.getByRole('main').getByRole('button', { name: /OIDC Smoke Provider/ })).toHaveCount(0);
    for (const sectionId of ['account-identity-heading', 'account-appearance-heading', 'account-language-heading']) {
      const section = page.getByRole('main').locator(`section[aria-labelledby="${sectionId}"]`);
      await expect(section).toHaveClass(/border-t/);
      await expect(section).not.toHaveClass(/rounded-2xl/);
      await expect(section).not.toHaveClass(/bg-raised/);
    }
    await expectNoAxeViolations(page, `auth-account-${width}`);
    await shot(page, dir, `auth-account-en-${width}.png`, capture, width);
    if (width === 390) {
      const focusedLight = await focusAppearanceChoiceByKeyboard(page, en['account.appearance.light']);
      await page.screenshot({ path: join(dir, 'auth-account-appearance-focus-light-en-390.png'), fullPage: true });
      await focusedLight.evaluate((element) => (element as HTMLElement).blur());
    }
    await applySavedDarkPreference(page);
    await expect(page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
    await expect(page.getByRole('main').locator('section[aria-labelledby="account-appearance-heading"]')).toBeVisible();
    await shot(page, dir, `auth-account-dark-en-${width}.png`, capture, width);
    if (width === 390) {
      await focusAppearanceChoiceByKeyboard(page, en['account.appearance.dark']);
      await page.screenshot({ path: join(dir, 'auth-account-appearance-focus-dark-en-390.png'), fullPage: true });
      await applySavedDarkPreference(page);
    }
    await applySystemAppearance(page);
    await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: cs['account.title'] })).toBeVisible();
    await shot(page, dir, `auth-account-cs-${width}.png`, capture, width);
    await accountContext.close();
  }

  const bootContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const boot = await bootContext.newPage();
  let releaseSession!: () => void;
  const sessionGate = new Promise<void>(resolve => { releaseSession = resolve; });
  await boot.route('**/api/v1/session', async route => { await sessionGate; await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }) }); });
  await boot.goto('/');
  await expect(boot.getByRole('status').filter({ hasText: en['app.loading'] })).toBeVisible();
  await shot(boot, dir, 'auth-boot-loading-en-390.png', capture, 390);
  releaseSession();
  await expect(boot.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
  await shot(boot, dir, 'auth-boot-unavailable-en-390.png', capture, 390);
  await bootContext.close();

  const signedInNotFoundContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await signedInNotFoundContext.addCookies(await page.context().cookies());
  const signedInNotFound = await signedInNotFoundContext.newPage();
  await signedInNotFound.goto('/unknown');
  await expect(signedInNotFound.getByRole('heading', { level: 1, name: en['app.notFound.title'] })).toBeVisible();
  await expectNoAxeViolations(signedInNotFound, 'auth-not-found-signed-in');
  await shot(signedInNotFound, dir, 'auth-not-found-signed-in-en-390.png', capture, 390);
  await signedInNotFoundContext.close();

  const notFoundContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  const notFound = await notFoundContext.newPage();
  await notFound.route('**/api/v1/session', route => route.fulfill({ status: 401, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unauthorized', status: 401 }) }));
  await notFound.route('**/api/v1/auth/setup', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ required: false }) }));
  await notFound.goto('/unknown');
  await expect(notFound.getByRole('heading', { level: 1, name: en['app.notFound.title'] })).toBeVisible();
  await expectNoAxeViolations(notFound, 'auth-not-found');
  await shot(notFound, dir, 'auth-not-found-signed-out-en-390.png', capture, 390);
  await notFound.getByRole('link', { name: en['app.notFound.signIn'], exact: true }).click();
  await expect(notFound.getByRole('heading', { level: 1, name: en['login.welcome'] })).toBeVisible();
  await notFoundContext.close();
}
