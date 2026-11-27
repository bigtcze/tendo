import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { URL as NodeURL, fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// A link drawn as a button takes the shared `buttonLook`, so it carries `data-variant` like every Button and the
// design metrics find a screen's primary action (#459 F22). Hand-copied button classes lose that marker silently.
// Node's URL: jsdom's global URL resolves relative paths against the document origin, not this module.
const srcRoot = fileURLToPath(new NodeURL('../../', import.meta.url));
const shared = path.join(srcRoot, 'components', 'ui', 'button.tsx');

function sources(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) return sources(full);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [full] : [];
  });
}

/** Class strings carrying the button's own size signature: the 44 px height with the 20 px button inset. */
function copiedButtonClasses(text: string): string[] {
  return [...text.matchAll(/(['"`])((?:(?!\1)[^\n])*)\1/g)]
    .map((match) => match[2])
    .filter((classes) => /(^|\s)min-h-11(\s|$)/.test(classes) && /(^|\s)px-5(\s|$)/.test(classes));
}

describe('button look usage', () => {
  it('draws the button look only through the shared button primitive', () => {
    const files = sources(srcRoot);
    // Guard against a vacuous pass: the scan reaches the primitive, and the primitive's base matches the signature.
    expect(files).toContain(shared);
    expect(files.length).toBeGreaterThan(50);
    expect(copiedButtonClasses(readFileSync(shared, 'utf8'))).toHaveLength(1);
    const copies = files.filter((file) => file !== shared && copiedButtonClasses(readFileSync(file, 'utf8')).length > 0)
      .map((file) => path.relative(srcRoot, file));
    expect(copies).toEqual([]);
  });
});
