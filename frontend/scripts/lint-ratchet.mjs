import { execFileSync } from 'node:child_process';
import { ESLint } from 'eslint';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import tseslint from 'typescript-eslint';
import { compareAgainstBase, compareMaintainability, maintainabilityPlugin, metricKey } from './lint-ratchet-core.mjs';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));
const BASELINE_PATH = resolve(ROOT, 'maintainability-baseline.json');
const baseline = JSON.parse(await readFile(BASELINE_PATH, 'utf8'));
const baseRef = process.argv.includes('--base-ref')
  ? process.argv[process.argv.indexOf('--base-ref') + 1]
  : process.env.MAINTAINABILITY_BASE_REF;
let baseBaseline;
if (baseRef) {
  try { baseBaseline = JSON.parse(execFileSync('git', ['show', `${baseRef}:frontend/maintainability-baseline.json`], { cwd: ROOT, encoding: 'utf8' })); }
  catch { /* no baseline at base is bootstrap */ }
}
const eslint = new ESLint({ cwd: ROOT, overrideConfigFile: true, overrideConfig: [ratchetConfig()] });

function ratchetConfig() {
  return {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
    linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' },
    languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
    plugins: { maintainability: maintainabilityPlugin },
    rules: { 'maintainability/metrics': 'error' },
  };
}
const results = await eslint.lintFiles(['src/**/*.{ts,tsx}']);
const tracked = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], { cwd: ROOT });
const sourceFiles = tracked.toString().split('\0').filter(isProductionSource);
const maintainabilityDirective = /eslint(?:-disable|-enable|-next-line|-line)?[^\n]*(?:complexity|max-depth|max-lines-per-function)/i;

function isProductionSource(file) {
  return file.startsWith('src/') && /\.tsx?$/.test(file)
    && !file.endsWith('.test.ts') && !file.endsWith('.test.tsx') && !file.startsWith('src/test/');
}

for (const file of sourceFiles) {
  if (await hasMaintainabilityDirective(file, maintainabilityDirective)) process.exitCode = 1;
}

async function hasMaintainabilityDirective(file, directive) {
  const content = await readFile(resolve(ROOT, file), 'utf8');
  if (!directive.test(content)) return false;
  console.error(`${file} contains an inline directive for a maintainability rule`);
  return true;
}
const measured = new Map();
for (const result of results) {
  for (const message of result.messages) {
    if (message.ruleId !== 'maintainability/metrics') continue;
    const payload = JSON.parse(message.message);
    for (const rule of payload.violations) {
      const key = metricKey(payload.file, payload.function, rule);
      measured.set(key, Math.max(measured.get(key) ?? 0, payload[rule]));
    }
  }
}
const errors = compareMaintainability(measured, baseline);
if (baseBaseline) errors.push(...compareAgainstBase(baseline, baseBaseline));
if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else if (!process.exitCode) {
  console.log(`ESLint function ratchet passed (${baseline.length} named exceptions).`);
}
