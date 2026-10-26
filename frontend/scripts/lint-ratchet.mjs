import { ESLint } from 'eslint';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, relative } from 'node:path';
import tseslint from 'typescript-eslint';
import { compareMaintainability, MAINTAINABILITY_THRESHOLDS } from './lint-ratchet-core.mjs';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));
const BASELINE_PATH = resolve(ROOT, 'maintainability-baseline.json');
const baseline = JSON.parse(await readFile(BASELINE_PATH, 'utf8'));
const eslint = new ESLint({
  cwd: ROOT,
  overrideConfigFile: true,
  overrideConfig: [
    {
      files: ['src/**/*.{ts,tsx}'],
      ignores: ['src/**/*.test.{ts,tsx}', 'src/test/**', 'src/**/generated/**', 'src/**/api-types.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      rules: {
        complexity: ['error', MAINTAINABILITY_THRESHOLDS.complexity],
        'max-depth': ['error', MAINTAINABILITY_THRESHOLDS['max-depth']],
        'max-lines-per-function': ['error', {
          max: MAINTAINABILITY_THRESHOLDS['max-lines-per-function'],
          skipBlankLines: true,
          skipComments: true,
          IIFEs: true,
        }],
      },
    },
  ],
});
const files = await eslint.lintFiles(['src/**/*.{ts,tsx}']);
const measured = new Map();
for (const result of files) {
  for (const message of result.messages) {
    if (!message.ruleId || !(message.ruleId in MAINTAINABILITY_THRESHOLDS)) continue;
    const match = message.message.match(/complexity of (\d+)|nesting depth of (\d+)|too many lines \((\d+)/i);
    if (!match) {
      console.error(`${relative(ROOT, result.filePath)} ${message.ruleId}: cannot parse metric: ${message.message}`);
      process.exitCode = 1;
      continue;
    }
    const file = relative(ROOT, result.filePath).replaceAll('\\', '/');
    const id = `${file}\0${message.ruleId}`;
    const value = Number(match.slice(1).find(Boolean));
    measured.set(id, Math.max(measured.get(id) ?? 0, value));
  }
}
const errors = compareMaintainability(measured, baseline);
if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else {
  console.log(`ESLint maintainability ratchet passed (${baseline.length} named exceptions).`);
}
