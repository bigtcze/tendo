import { expect, test, type Page } from '@playwright/test';
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
  const root = page.locator('[data-switch-root]');
  const track = root.locator('span[aria-hidden="true"]');
  const input = page.getByRole('switch', { name: 'Primitive switch' });
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
