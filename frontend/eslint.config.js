import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'test-results', 'playwright-report'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
    linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' },
    rules: {
      complexity: ['error', 15],
      'max-depth': ['error', 4],
      'max-lines-per-function': ['error', { max: 80, skipBlankLines: true, skipComments: true, IIFEs: true }],
      'max-lines': ['error', { max: 350, skipBlankLines: true, skipComments: false }],
    },
  },
  {
    files: ['src/features/items/useHomeItems.ts'],
    // Effect starts async loading; resulting state updates follow network responses. Owner: frontend-items.
    rules: { 'react-hooks/set-state-in-effect': 'off' },
  },
  {
    files: ['src/features/items/useLabelRetryFocus.ts'],
    // Effect settles the retry once its batch completes. Owner: frontend-items.
    rules: { 'react-hooks/set-state-in-effect': 'off' },
  },
  {
    files: ['src/i18n/{cs,en}.ts'],
    // Flat translation tables stay together for translators; owner: frontend-localization.
    rules: { 'max-lines': 'off' },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: { ...globals.browser } },
  },
  {
    files: ['*.config.{js,ts}', 'scripts/**/*.mjs'],
    languageOptions: { globals: { ...globals.node } },
  },
);
