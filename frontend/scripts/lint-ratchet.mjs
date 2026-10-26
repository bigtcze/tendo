import { execFileSync } from 'node:child_process';
import { ESLint } from 'eslint';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import tseslint from 'typescript-eslint';
import { compareAgainstBase, compareMaintainability, loadBaseBaseline, maintainabilityPlugin, measurementsFromResults } from './lint-ratchet-core.mjs';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));
const BASELINE_PATH = resolve(ROOT, 'maintainability-baseline.json');
const baseline = JSON.parse(await readFile(BASELINE_PATH, 'utf8'));
const baseRef = process.argv.includes('--base-ref')
  ? process.argv[process.argv.indexOf('--base-ref') + 1]
  : process.env.MAINTAINABILITY_BASE_REF;
let baseBaseline;
if (baseRef) {
  try {
    baseBaseline = loadBaseBaseline(baseRef, ROOT);
  } catch (error) {
    console.error(`cannot load maintainability baseline at ${baseRef}: ${error.message}`);
    process.exit(1);
  }
}
const eslint = new ESLint({ cwd: ROOT, overrideConfigFile: true, overrideConfig: [ratchetConfig()] });
const allowedInlineRule = 'react-hooks/set-state-in-effect';

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
for (const result of results) {
  const fatal = result.messages.filter(message => message.fatal
    || (message.severity === 2 && message.ruleId !== 'maintainability/metrics'));
  const unexpectedInline = result.messages.filter(message => message.ruleId === null
    && message.messageId === 'inlineConfig'
    && !message.message.includes(allowedInlineRule));
  fatal.push(...unexpectedInline);
  for (const message of fatal) {
    console.error(`${result.filePath}:${message.line ?? 0} ${message.message}`);
    process.exitCode = 1;
  }
}
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
const { measured, errors: attributionErrors } = measurementsFromResults(results);
for (const error of attributionErrors) {
  console.error(error);
  process.exitCode = 1;
}
const errors = compareMaintainability(measured, baseline);
if (baseBaseline) errors.push(...compareAgainstBase(baseline, baseBaseline));
if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else if (!process.exitCode) {
  console.log(`ESLint function ratchet passed (${baseline.length} named exceptions).`);
}
