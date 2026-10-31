import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import type { Locator, Page } from '@playwright/test';

export function activateBrowserWindow() {
  const windows = execFileSync('xdotool', ['search', '--onlyvisible', '--name', 'Google Chrome for Testing'], { encoding: 'utf8' }).trim().split(/\s+/).filter(Boolean);
  for (const id of windows) {
    const pid = execFileSync('xdotool', ['getwindowpid', id], { encoding: 'utf8' }).trim();
    const owned = execFileSync('xdotool', ['search', '--onlyvisible', '--pid', pid], { encoding: 'utf8' }).trim().split(/\s+/);
    if (!owned.includes(id)) continue;
    execFileSync('xdotool', ['windowactivate', '--sync', id]);
    execFileSync('xdotool', ['windowfocus', '--sync', id]);
    const active = execFileSync('xdotool', ['getactivewindow'], { encoding: 'utf8' }).trim();
    if (active !== id) throw new Error(`Playwright Chromium window failed activation: expected ${id}, active ${active}`);
    return;
  }
  throw new Error('No visible Playwright Chromium window owned by its browser process');
}

export async function realClick(page: Page, target: Locator) {
  activateBrowserWindow();
  const box = await target.boundingBox();
  if (!box) throw new Error('Real-X click target is not visible');
  const point = await page.evaluate(({ x, y }) => ({
    x: Math.round(window.screenX + (window.outerWidth - window.innerWidth) + x),
    y: Math.round(window.screenY + (window.outerHeight - window.innerHeight) + y),
  }), { x: box.x + box.width / 2, y: box.y + box.height / 2 });
  execFileSync('xdotool', ['mousemove', '--sync', String(point.x), String(point.y), 'click', '1']);
  const activeWindow = execFileSync('xdotool', ['getactivewindow'], { encoding: 'utf8' }).trim();
  const activePid = execFileSync('xdotool', ['getwindowpid', activeWindow], { encoding: 'utf8' }).trim();
  const owned = execFileSync('xdotool', ['search', '--onlyvisible', '--pid', activePid], { encoding: 'utf8' }).trim().split(/\s+/);
  if (!owned.includes(activeWindow)) throw new Error(`Real-X click left the Playwright browser window: ${activeWindow}`);
  return page.evaluate(() => document.activeElement instanceof HTMLElement ? {
    tag: document.activeElement.tagName.toLowerCase(),
    id: document.activeElement.id || null,
    name: document.activeElement.getAttribute('aria-label') || document.activeElement.textContent?.trim().slice(0, 100) || null,
  } : null);
}

// Precondition only: the X-active window is the Playwright browser and its document holds focus on a
// real control. The caller asserts the post-key destination; checking it here would invert the contract.
export async function realKey(page: Page, key: string) {
  activateBrowserWindow();
  const source = await page.evaluate(() => document.hasFocus() && document.activeElement instanceof HTMLElement
    && document.activeElement !== document.body ? document.activeElement.tagName.toLowerCase() : null);
  if (!source) throw new Error(`Refusing real-X ${key}: browser document has no focused control`);
  execFileSync('xdotool', ['key', '--clearmodifiers', key]);
}

export function speechLines(debugPath: string, offset: number) {
  return readFileSync(debugPath).subarray(offset).toString('utf8').split(/\r?\n/)
    .filter((line) => line.includes('SPEECH OUTPUT:'))
    .map((line) => line.match(/SPEECH OUTPUT:\s*'((?:\\.|[^'])*)'/)?.[1]?.replace(/\\'/g, "'").trim() ?? '')
    .filter(Boolean);
}
