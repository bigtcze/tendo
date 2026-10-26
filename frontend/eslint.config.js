import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';
import maintainabilityBaseline from './maintainability-baseline.json' with { type: 'json' };
import { MAINTAINABILITY_THRESHOLDS } from './scripts/lint-ratchet-core.mjs';

const baselineOverrides = maintainabilityBaseline.map(({ file, rule, allowed }) => ({
  files: [file],
  rules: {
    [rule]: rule === 'max-lines-per-function'
      ? ['error', {
          max: allowed,
          skipBlankLines: true,
          skipComments: true,
          IIFEs: true,
        }]
      : ['error', allowed],
  },
}));

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'test-results', 'playwright-report'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}', 'src/test/**', 'src/**/generated/**', 'src/**/api-types.ts'],
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
  // Exceptions are measured and ratcheted by scripts/lint-ratchet.mjs.
  ...baselineOverrides,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: { ...globals.browser } },
  },
  {
    files: ['*.config.{js,ts}', 'scripts/**/*.mjs'],
    languageOptions: { globals: { ...globals.node } },
  },
);
