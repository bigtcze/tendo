import { expect, test, type Page } from '@playwright/test';
import { focusIndicatorSignature, hasPaintedFocusIndicator } from './focusIndicator';
import { createTabRepeatGuard } from './tabRepeatGuard';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';

const frontendRoot = fileURLToPath(new URL('..', import.meta.url));
const harnessRoot = path.join(frontendRoot, 'e2e/harness');
let outputDirectory: string;

async function openHarness(page: Page, query = '') {
  await page.route('http://harness.local/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    const filePath = path.resolve(outputDirectory, `.${pathname === '/' ? '/index.html' : pathname}`);
    if (!filePath.startsWith(`${outputDirectory}${path.sep}`) && filePath !== path.join(outputDirectory, 'index.html')) {
      await route.fulfill({ status: 404 });
      return;
    }

    try {
      const body = await readFile(filePath);
      const contentType = filePath.endsWith('.css')
        ? 'text/css'
        : filePath.endsWith('.js')
          ? 'text/javascript'
          : 'text/html';
      await route.fulfill({ status: 200, contentType, body });
    } catch {
      await route.fulfill({ status: 404 });
    }
  });
  await page.goto(`http://harness.local/${query}`);
}

test.beforeAll(async () => {
  outputDirectory = await mkdtemp(path.join(tmpdir(), 'tendo-primitives-'));
  await build({
    configFile: false,
    root: harnessRoot,
    plugins: [react(), tailwindcss()],
    build: { outDir: outputDirectory, emptyOutDir: true, assetsDir: 'assets' },
  });
});

test.afterAll(async () => {
  if (outputDirectory) await rm(outputDirectory, { recursive: true, force: true });
});

test('Tab repeat guard distinguishes trapped controls from native date segments in a real browser', async ({ page }) => {
  await page.setContent(`<button id="before">Before</button><button id="trapped">Trapped</button><button id="after">After</button>
    <script>document.querySelector('#trapped').addEventListener('keydown', event => { if (event.key === 'Tab') event.preventDefault(); });</script>`);
  await page.locator('#before').focus();
  const trappedGuard = createTabRepeatGuard(3);
  await page.keyboard.press('Tab');
  const trappedId = await page.evaluate(() => (document.activeElement as HTMLElement).id);
  expect(trappedGuard.observe(trappedId, await page.evaluate(() => document.activeElement?.matches('input[type="date"]') ?? false))).toBe('new');
  await page.keyboard.press('Tab');
  const stillTrappedId = await page.evaluate(() => (document.activeElement as HTMLElement).id);
  expect(stillTrappedId).toBe('trapped');
  expect(trappedGuard.observe(stillTrappedId, false)).toBe('trap');

  await page.setContent('<button id="date-before">Before</button><input id="date" type="date"><button id="date-after">After</button>');
  await page.locator('#date-before').focus();
  const dateGuard = createTabRepeatGuard(3);
  const results: string[] = [];
  let current = '';
  for (let i = 0; i < 5 && current !== 'date-after'; i += 1) {
    await page.keyboard.press('Tab');
    current = await page.evaluate(() => (document.activeElement as HTMLElement).id);
    const isDate = await page.evaluate(() => document.activeElement?.matches('input[type="date"]') ?? false);
    results.push(dateGuard.observe(current, isDate));
  }
  expect(results).toContain('segment');
  expect(results.every((result) => result === 'new' || result === 'segment')).toBe(true);
  expect(current).toBe('date-after');
  expect(results).not.toContain('trap');

  // Cap semantics: 3 additional presses on one date input are segments; the 4th additional press is a trap.
  const capGuard = createTabRepeatGuard(3);
  expect(Array.from({ length: 5 }, () => capGuard.observe('date', true))).toEqual(['new', 'segment', 'segment', 'segment', 'trap']);
});

test('focus indicator helper distinguishes visible and absent focus paint in a real browser', async ({ page }) => {
  await page.setContent(`<style>
    #invisible:focus { outline: none; box-shadow: none; }
    #visible:focus { outline: 3px solid rgb(0, 120, 255); }
  </style><button id="invisible">Invisible</button><button id="visible">Visible</button>`);
  const invisible = page.locator('#invisible');
  await invisible.focus();
  expect(await invisible.evaluate(hasPaintedFocusIndicator)).toBe(false);
  expect(await invisible.evaluate(focusIndicatorSignature)).toBe(await page.locator('#visible').evaluate(focusIndicatorSignature));
  const visible = page.locator('#visible');
  await visible.focus();
  expect(await visible.evaluate(hasPaintedFocusIndicator)).toBe(true);
  // The focused signature must differ from the same element's unfocused signature.
  expect(await visible.evaluate(focusIndicatorSignature)).not.toBe(await invisible.evaluate(focusIndicatorSignature));
});

test('native dialog close synchronizes controlled state and can reopen', async ({ page }) => {
  await openHarness(page);
  const dialog = page.getByRole('dialog', { name: 'Primitive dialog' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Native close' }).click();
  await expect(page.getByTestId('dialog-state')).toHaveText('closed');
  await expect(dialog).not.toBeVisible();

  await page.getByRole('button', { name: 'Reopen dialog' }).click();
  await expect(page.getByTestId('dialog-state')).toHaveText('open');
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveJSProperty('open', true);
});

test('native dialog close is refused while busy', async ({ page }) => {
  await openHarness(page, '?busy');
  const dialog = page.getByRole('dialog', { name: 'Primitive dialog' });
  await dialog.getByRole('button', { name: 'Native close' }).click();

  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveJSProperty('open', true);
  await expect(page.getByTestId('dialog-state')).toHaveText('open');
});

test('switch track endpoints are covered by a full-size native input', async ({ page }) => {
  await openHarness(page, '?closed');
  const input = page.getByRole('switch', { name: 'Primitive switch', exact: true });
  const root = input.locator('xpath=..');
  const track = root.locator('span[aria-hidden="true"]');
  const trackBox = await track.boundingBox();
  const inputBox = await input.boundingBox();
  expect(trackBox).not.toBeNull();
  expect(inputBox).not.toBeNull();
  if (!trackBox || !inputBox) throw new Error('Switch track/input has no browser layout box');

  expect(trackBox.width).toBe(56);
  expect(inputBox.height).toBeGreaterThanOrEqual(44);
  expect(inputBox.width).toBeGreaterThanOrEqual(trackBox.width);
  await expect(input).toHaveAttribute('aria-checked', 'false');

  const leftPoint = { x: trackBox.x + 3, y: trackBox.y + trackBox.height / 2 };
  const rightPoint = { x: trackBox.x + trackBox.width - 3, y: trackBox.y + trackBox.height / 2 };
  for (const point of [leftPoint, rightPoint]) {
    const hitTarget = await page.evaluate(({ x, y }) => {
      const element = document.elementFromPoint(x, y);
      return { isInput: element === document.querySelector('[data-switch-root] input'), element: element?.outerHTML };
    }, point);
    expect(hitTarget.isInput, `Unexpected hit target: ${hitTarget.element}`).toBe(true);

    await page.mouse.click(point.x, point.y);
    await expect(input).toHaveAttribute('aria-checked', 'true');
    await expect(input).toBeChecked();
    await page.mouse.click(point.x, point.y);
    await expect(input).toHaveAttribute('aria-checked', 'false');
    await expect(input).not.toBeChecked();
  }
});

test('switch track hover comes from the input and is suppressed while disabled', async ({ page }) => {
  await openHarness(page, '?closed');
  const trackColor = (name: string) => page.getByRole('switch', { name, exact: true }).evaluate((input) => getComputedStyle(input.nextElementSibling!).backgroundColor);
  const field = 'rgb(107, 124, 144)';
  const muted = 'rgb(82, 100, 123)';
  const accent = 'rgb(0, 109, 130)';
  const accentStrong = 'rgb(0, 85, 102)';

  const unchecked = page.getByRole('switch', { name: 'Primitive switch', exact: true });
  await expect.poll(() => trackColor('Primitive switch')).toBe(field);
  await unchecked.hover();
  await expect.poll(() => trackColor('Primitive switch'), { message: 'unchecked track darkens on input hover' }).toBe(muted);
  await page.mouse.move(0, 0);
  await expect.poll(() => trackColor('Primitive switch')).toBe(field);

  const checked = page.getByRole('switch', { name: 'Checked primitive switch', exact: true });
  await expect.poll(() => trackColor('Checked primitive switch')).toBe(accent);
  await checked.hover();
  await expect.poll(() => trackColor('Checked primitive switch'), { message: 'checked track darkens on input hover' }).toBe(accentStrong);
  await page.mouse.move(0, 0);
  await expect.poll(() => trackColor('Checked primitive switch')).toBe(accent);

  const disabled = page.getByRole('switch', { name: 'Disabled primitive switch', exact: true });
  await disabled.hover();
  await expect.poll(() => trackColor('Disabled primitive switch'), { message: 'disabled unchecked track ignores hover' }).toBe(field);
  await page.mouse.move(0, 0);

  const disabledChecked = page.getByRole('switch', { name: 'Disabled checked primitive switch', exact: true });
  await disabledChecked.hover();
  await expect.poll(() => trackColor('Disabled checked primitive switch'), { message: 'disabled checked track ignores hover' }).toBe(accent);
});
