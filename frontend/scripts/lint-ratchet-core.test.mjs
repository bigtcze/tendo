import assert from 'node:assert/strict';
import test from 'node:test';
import {
  compareAgainstBase,
  compareMaintainability,
  measurementsFromResults,
  loadBaseBaseline,
  metricKey,
  maintainabilityPlugin,
} from './lint-ratchet-core.mjs';
import { ESLint } from 'eslint';
import tseslint from 'typescript-eslint';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const entry = (allowed = 20, reason = 'technical exception') => ({
  file: 'src/example.ts', function: 'Example', rule: 'complexity', allowed, reason, owner: 'frontend-app',
});
const fn = 'Example';
const observed = (value) => [[metricKey('src/example.ts', fn, 'complexity'), value]];
const complexityByIdentity = (measured) => Object.fromEntries([...measured]
  .filter(([key]) => key.endsWith('\0complexity'))
  .map(([key, value]) => [key.split('\0')[1], value]));

test('function exceptions are independently keyed by qualified function', () => {
  const first = metricKey('src/example.ts', 'Screen.first', 'complexity');
  const second = metricKey('src/example.ts', 'Screen.second', 'complexity');
  const baseline = [{ ...entry(20), function: 'Screen.first' }];
  const errors = compareMaintainability(new Map([[first, 20], [second, 21]]), baseline);
  assert.match(errors.join('\n'), /Screen.second complexity measured=21/);
  const improvedFirst = new Map([[first, 18], [second, 21]]);
  const tightenedFirst = [{ ...baseline[0], allowed: 18 }];
  assert.equal(compareMaintainability(improvedFirst, tightenedFirst).length, 1);
});

test('ESLint API reports violations despite inline disable and rule override', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-ratchet-'));
  try {
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' },
      languageOptions: {
        parser: tseslint.parser,
        parserOptions: { sourceType: 'module', ecmaVersion: 'latest' },
      },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    for (const [name, directive] of [
      ['disable.ts', '/* eslint-disable complexity */'],
      ['override.ts', '/* eslint complexity: ["error", 1000] */'],
    ]) {
      const path = join(directory, 'src', name);
      const { mkdir } = await import('node:fs/promises');
      await mkdir(join(directory, 'src'), { recursive: true });
      const branches = Array.from({ length: 20 }, (_, index) => `if (value === ${index}) return ${index};`).join('\n');
      await writeFile(path, `${directive}\nfunction fresh() {\n${branches}\n}`);
      const [result] = await eslint.lintFiles([path]);
      assert.ok(result.messages.some(message => message.ruleId === 'maintainability/metrics'), name);
      assert.ok(
        result.messages.some(message => message.ruleId === null && /noInlineConfig|inline config/i.test(message.message)),
        name,
      );
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('delegated ESLint rules preserve core complexity and report attribution', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-core-semantics-'));
  try {
    const { mkdir } = await import('node:fs/promises');
    await mkdir(join(directory, 'src'), { recursive: true });
    const path = join(directory, 'src/core.ts');
    const branches = Array.from({ length: 18 }, (_, index) => `  if (value && value?.valueOf() === ${index}) { if (value) { if (value) { count += value ? (value ?? false) : 0; } } }`).join('\n');
    const padding = Array.from({ length: 82 }, (_, index) => `  count += ${index};`).join('\n');
    await writeFile(path, `function measured(value = true) {\n  let count = value ? 1 : 0;\n  count ??= 2;\n  count ||= 3;\n  for (const key in { a: 1 }) {\n    try {\n${branches}\n    } catch (error) {\n      count += error ? 1 : 0;\n    }\n  }\n${padding}\n  return count ?? value?.valueOf();\n}\nfunction parameterClosure(callback = (value) => value ? value ?? false : value?.valueOf()) {\n  return callback(false);\n}\n`);
    const languageOptions = { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } };
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'], languageOptions,
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const core = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'], languageOptions,
      rules: { complexity: ['error', 0], 'max-depth': ['error', 0], 'max-lines-per-function': ['error', { max: 0, skipBlankLines: true, skipComments: true, IIFEs: true }] },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const [coreResult] = await core.lintFiles([path]);
    const { measured } = measurementsFromResults([result]);
    const coreMetrics = new Map();
    for (const message of coreResult.messages) {
      const rule = message.ruleId;
      const value = message.message.includes('complexity') ? message.message.match(/complexity of (\d+)/)?.[1]
        : rule === 'max-depth' ? message.message.match(/too deeply \((\d+)\)/)?.[1]
          : message.message.match(/lines \((\d+)\)/)?.[1];
      assert.ok(value, `${rule} message had a parseable metric: ${message.message}`);
      const key = `${message.node?.type}:${message.line}:${rule}`;
      coreMetrics.set(key, Number(value));
    }
    const actualComplexity = measured.get(metricKey('src/core.ts', 'measured', 'complexity'));
    const actualDepth = measured.get(metricKey('src/core.ts', 'measured', 'max-depth'));
    const actualLines = measured.get(metricKey('src/core.ts', 'measured', 'max-lines-per-function'));
    assert.ok(actualComplexity > 0);
    assert.ok(!result.messages.some(message => message.message.includes('Unable to attribute')));
    const complexity = Math.max(...coreResult.messages.filter(message => message.ruleId === 'complexity').map(message => Number(message.message.match(/complexity of (\d+)/)?.[1] ?? 0)));
    const depth = Math.max(...coreResult.messages.filter(message => message.ruleId === 'max-depth').map(message => Number(message.message.match(/too deeply \((\d+)\)/)?.[1] ?? 0)));
    const linesMessage = coreResult.messages.find(message => message.ruleId === 'max-lines-per-function' && message.message.includes('measured'));
    const lines = Number(linesMessage.message.match(/lines \((\d+)\)/)?.[1]);
    assert.equal(actualComplexity, complexity);
    assert.equal(actualDepth, depth);
    assert.equal(actualLines, lines);
    assert.ok(coreMetrics.size >= 3, 'core rules report the fixture code paths and rules');
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('class field initializers and static blocks have independent identities', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-implicit-functions-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/implicit.ts');
    const branches = Array.from({ length: 17 }, (_, index) => `if (value === ${index}) return ${index};`).join('\n');
    await writeFile(path, `class Example {\n  method(value: number) {\n    return value;\n  }\n  field = (() => {\n${branches}\n    return 0;\n  })();\n  static {\n${branches}\n  }\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.ok(measured.has(metricKey('src/implicit.ts', 'Example.<field:field>.anonymous#1', 'complexity')));
    assert.ok(measured.has(metricKey('src/implicit.ts', 'Example.<static-block#1>', 'complexity')));
    assert.ok(!measured.has(metricKey('src/implicit.ts', 'Example.method', 'complexity')));
    assert.ok(!result.messages.some(message => message.message.includes('Unable to attribute')));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('member decorators are measured once in method or field scope', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-member-decorators-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/decorators.ts');
    const branches = count => Array.from({ length: count - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `function decorate(value: any) { return value; }\nclass Decorated {\n@decorate(() => { if (true) return 1; })\nsmall() { return 0; }\n@decorate(() => {\n${branches(18)}\nreturn 1;\n})\nlarge() { return 0; }\n@decorate(() => {\n${branches(19)}\nreturn 1;\n})\nfield = 1;\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest', experimentalDecorators: true } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      'Decorated.<method:large>.decorator#1.anonymous#1': 18,
      'Decorated.<field:field>.decorator#1.anonymous#1': 19,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('anonymous parameter functions share their function owner ordinal sequence', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-shared-param-counts-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/parameters.ts');
    const branches = count => Array.from({ length: count - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `function f(a = (value: number) => {\n${branches(17)}\nreturn 1;\n}, b = (value: number) => {\n${branches(18)}\nreturn 2;\n}) {\n  return [a, b];\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), { 'f.anonymous#1': 17, 'f.anonymous#2': 18 });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('field arrow callbacks own their own reports', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-field-arrow-owner-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/arrow.ts');
    const branches = Array.from({ length: 19 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `class ArrowField {\n  callback = (value: number) => {\n${branches}\n    return 0;\n  };\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.equal(measured.get(metricKey('src/arrow.ts', 'ArrowField.<field:callback>.callback', 'complexity')), 20);
    assert.equal(measured.has(metricKey('src/arrow.ts', 'ArrowField.<field:callback>', 'complexity')), false);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('static block statements share one owner ordinal sequence', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-static-block-counts-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/static-counts.ts');
    const branches = count => Array.from({ length: count - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `const use = (callback: unknown) => callback;\nclass StaticCounts {\nstatic {\nuse((value: number) => {\n${branches(17)}\nreturn 1;\n});\nuse((value: number) => {\n${branches(18)}\nreturn 2;\n});\n}\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      'StaticCounts.<static-block#1>.anonymous#1': 17,
      'StaticCounts.<static-block#1>.anonymous#2': 18,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('computed method keys and functions in defaults receive separate identities', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-key-functions-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/key-functions.ts');
    const branches = Array.from({ length: 19 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `const object = { [(() => {\n${branches}\nreturn 'field';\n})()]: 1, [((value = (() => {\n${branches}\nreturn 1;\n})()) => value)()]: 2, [function computedKey() {\n${branches}\nreturn 'method';\n}]() {\n${branches}\nreturn 0;\n} };\nfunction withDefault(callback = (() => {\n${branches}\nreturn 1;\n})) { return callback; }\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      'object.key#1.anonymous#1': 20,
      'object.key#2.anonymous#1.anonymous#1': 20,
      'object.key#3.anonymous#1': 20,
      'object.computed#3': 20,
      'withDefault.anonymous#1': 20,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('anonymous exports and nested classes receive lexical identities', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-exports-and-nesting-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/exports.ts');
    const branches = Array.from({ length: 18 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `export default function(value: number) {\n${branches}\nreturn 0;\n}\nexport default class {\nmethod(value: number) {\n${branches}\nreturn 0;\n}\nNested = class { run(value: number) {\n${branches}\nreturn 0;\n} };\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      default: 19,
      'default.<method:method>': 19,
      'default.<field:Nested>.class#1.<method:run>': 19,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('accessors and object methods have distinct semantic identities', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-accessors-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/accessors.ts');
    const branches = count => Array.from({ length: count - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `class Accessors {\nget value() {\n${branches(17)}\nreturn 0;\n}\nset value(value: number) {\n${branches(18)}\n}\nstatic get status() {\n${branches(19)}\nreturn 0;\n}\nrun = function() {\n${branches(20)}\nreturn 0;\n};\nrun() {\n${branches(21)}\nreturn 0;\n}\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      'Accessors.<get:value>': 17,
      'Accessors.<set:value>': 18,
      'Accessors.static.<get:status>': 19,
      'Accessors.<field:run>.run': 20,
      'Accessors.<method:run>': 21,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('class initializer identities stay unique across modes and computed keys', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-fields-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/fields.ts');
    const branch = n => Array.from({ length: n - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join('\n');
    await writeFile(path, `class Fields {\nstatic foo = () => {\n${branch(21)}\nreturn 0; }; foo = () => {\n${branch(17)}\nreturn 0; };\nfirst = (() => {\n${branch(18)}\nreturn 0; })();\nsecond = (() => {\n${branch(16)}\nreturn 0; })();\n[Fields.key()] = (() => {\n${branch(19)}\nreturn 0; })();\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.deepEqual(complexityByIdentity(measured), {
      'Fields.<static-field:foo>.foo': 21,
      'Fields.<field:foo>.foo': 17,
      'Fields.<field:first>.anonymous#1': 18,
      'Fields.<field:second>.anonymous#1': 16,
      'Fields.<field:computed#1>.anonymous#1': 19,
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('same-line fields and method receive separate effective identities', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-one-line-class-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/one-line.ts');
    const branches = count => Array.from({ length: count - 1 }, (_, i) => `if (value === ${i}) return ${i};`).join(' ');
    await writeFile(path, `class OneLine { first = (() => { ${branches(21)} return 0; })(); second = (() => { ${branches(17)} return 0; })(); method(value: number) { ${branches(19)} return 0; } }`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin }, rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const { measured, errors } = measurementsFromResults([result]);
    assert.deepEqual(errors, []);
    assert.equal(measured.get(metricKey('src/one-line.ts', 'OneLine.<field:first>.anonymous#1', 'complexity')), 21);
    assert.equal(measured.get(metricKey('src/one-line.ts', 'OneLine.<field:second>.anonymous#1', 'complexity')), 17);
    assert.ok([...measured.keys()].some(key => key.includes('OneLine.<method:method>') && key.endsWith('\0complexity')));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('nested src path segments remain part of file identity', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-nested-src-'));
  try {
    const { mkdir } = await import('node:fs/promises');
    const nested = join(directory, 'src/new/src/app');
    const normal = join(directory, 'src/app');
    await mkdir(nested, { recursive: true });
    await mkdir(normal, { recursive: true });
    const lines = count => Array.from({ length: count }, (_, index) => `if (value === ${index}) return ${index};`).join('\n');
    await writeFile(join(nested, 'App.tsx'), `export function App(value: number) {\n${lines(20)}\nreturn 0; }\n`);
    await writeFile(join(normal, 'App.tsx'), `export function App(value: number) {\n${lines(18)}\nreturn 0; }\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.tsx'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    const results = await eslint.lintFiles(['src/**/*.tsx']);
    const { measured } = measurementsFromResults(results);
    assert.ok(measured.has(metricKey('src/new/src/app/App.tsx', 'App', 'complexity')));
    assert.ok(measured.has(metricKey('src/app/App.tsx', 'App', 'complexity')));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('base comparison rejects new or raised entries and bootstraps without baseline', () => {
  const current = [{ ...entry(21), function: 'Example' }];
  const base = [{ ...entry(20), function: 'Example' }];
  assert.match(compareAgainstBase(current, base)[0], /allowed raised=21 base=20/);
  assert.match(compareAgainstBase(current, [])[0], /new exception relative to base/);
  assert.deepEqual(compareAgainstBase([], []), []);
});

test('duplicate owner identities are detected and rejected', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-duplicate-owner-'));
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'src'), { recursive: true }));
    const path = join(directory, 'src/duplicate.ts');
    await writeFile(path, `function same() { if (true) return 1; }\nfunction same() { if (true) return 2; }\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    assert.ok(measurementsFromResults([result]).errors.some(error => error.includes('duplicate function identity')));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});


test('base loader bootstraps only when a resolved ref has no baseline', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'tendo-base-loader-'));
  try {
    const { execFileSync } = await import('node:child_process');
    const git = (...args) => execFileSync('git', args, { cwd: directory, stdio: 'ignore' });
    git('init', '-q');
    git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'empty');
    assert.equal(loadBaseBaseline('HEAD', directory), null);
    assert.throws(() => loadBaseBaseline('missing-ref', directory));
    await import('node:fs/promises').then(({ mkdir }) => mkdir(join(directory, 'frontend'), { recursive: true }));
    await writeFile(join(directory, 'frontend/maintainability-baseline.json'), JSON.stringify([{ file: 'src/a.ts', function: 'A', rule: 'complexity', allowed: 15 }]));
    git('add', 'frontend/maintainability-baseline.json');
    git('commit', '-qm', 'valid baseline');
    const frontendDirectory = join(directory, 'frontend');
    assert.equal(loadBaseBaseline('HEAD', frontendDirectory)[0].allowed, 15);
    assert.match(compareAgainstBase([{ file: 'src/a.ts', function: 'A', rule: 'complexity', allowed: 16 }], loadBaseBaseline('HEAD', frontendDirectory))[0], /allowed raised=16 base=15/);
    await writeFile(join(directory, 'frontend/maintainability-baseline.json'), '{bad');
    git('add', 'frontend/maintainability-baseline.json');
    git('commit', '-qm', 'malformed baseline');
    assert.throws(() => loadBaseBaseline('HEAD', frontendDirectory), /malformed baseline/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('accepts unchanged exception measurement', () => {
  assert.deepEqual(compareMaintainability(observed(20), [entry()]), []);
});

test('reports new violations and worsening', () => {
  assert.match(compareMaintainability(observed(21), [])[0], /measured=21/);
  assert.match(compareMaintainability(observed(21), [entry()])[0], /measured=21 allowed=20/);
});

test('reports stale, missing reason, and duplicate exceptions', () => {
  assert.match(compareMaintainability(observed(19), [entry()])[0], /stale measured=19/);
  assert.match(compareMaintainability(observed(20), [entry(20, '')])[0], /missing reason/);
  assert.match(compareMaintainability(observed(20), [{ ...entry(), owner: 'TODO: assign' }])[0], /missing owner/);
  assert.match(compareMaintainability(observed(20), [{ ...entry(), reason: 'TODO: replace' }])[0], /missing reason/);
  assert.match(compareMaintainability(observed(20), [entry(), entry()])[0], /duplicate exception/);
  assert.match(compareMaintainability([], [entry()])[0], /no violation/);
});
