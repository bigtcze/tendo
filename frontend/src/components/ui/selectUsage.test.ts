import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { URL as NodeURL, fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Every select in the product goes through the shared Select primitive, so none falls back to the platform arrow
// beside the app's own chevrons (#459 design finding "native selects beside custom controls").
// Node's URL: jsdom's global URL resolves relative paths against the document origin, not this module.
const srcRoot = fileURLToPath(new NodeURL('../../', import.meta.url));

function sources(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) return sources(full);
    return /\.tsx$/.test(entry.name) && !/\.test\.tsx$/.test(entry.name) ? [full] : [];
  });
}

describe('select usage', () => {
  it('renders no raw <select> outside the shared Select primitive', () => {
    const shared = path.join(srcRoot, 'components', 'ui', 'select.tsx');
    const files = sources(srcRoot);
    // Guard against a vacuous pass: the scan must reach the primitive itself and the feature screens.
    expect(files).toContain(shared);
    expect(files.length).toBeGreaterThan(50);
    expect(/<select\b/.test(readFileSync(shared, 'utf8'))).toBe(true);
    const raw = files.filter((file) => file !== shared && /<select\b|createElement\(\s*['"]select['"]/.test(readFileSync(file, 'utf8')));
    expect(raw).toEqual([]);
  });
});
