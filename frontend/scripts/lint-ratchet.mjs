import { ESLint } from 'eslint';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import tseslint from 'typescript-eslint';
import { compareMaintainability, maintainabilityPlugin, metricKey } from './lint-ratchet-core.mjs';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));
const BASELINE_PATH = resolve(ROOT, 'maintainability-baseline.json');
const baseline = JSON.parse(await readFile(BASELINE_PATH, 'utf8'));
const eslint = new ESLint({
  cwd: ROOT,
  overrideConfigFile: true,
  overrideConfig: [{
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
    linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' },
    languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
    plugins: { maintainability: maintainabilityPlugin },
    rules: { 'maintainability/metrics': 'error' },
  }],
});
const results = await eslint.lintFiles(['src/**/*.{ts,tsx}']);
const measured = new Map();
for (const result of results) {
  for (const message of result.messages) {
    if (message.ruleId === 'null' && message.message.includes('eslint')) {
      console.error(`${result.filePath}: inline ESLint configuration is forbidden in production source: ${message.message}`);
      process.exitCode = 1;
    }
    if (message.ruleId !== 'maintainability/metrics') continue;
    const payload = JSON.parse(message.message);
    for (const rule of payload.violations) {
      const key = metricKey(payload.file, payload.function, rule);
      measured.set(key, Math.max(measured.get(key) ?? 0, payload[rule]));
    }
  }
}
const errors = compareMaintainability(measured, baseline);
if (errors.length) { console.error(errors.join('\n')); process.exitCode = 1; }
else console.log(`ESLint function ratchet passed (${baseline.length} named exceptions).`);
