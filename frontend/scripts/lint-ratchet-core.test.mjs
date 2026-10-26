import assert from 'node:assert/strict';
import test from 'node:test';
import {
  compareAgainstBase,
  compareMaintainability,
  collectFunctionNodes,
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
    const reports = result.messages.filter(message => message.ruleId === 'maintainability/metrics').map(message => JSON.parse(message.message));
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
    const measured = Object.assign({}, ...reports.filter(payload => payload.function === 'measured'));
    assert.ok(measured.function, JSON.stringify(reports));
    assert.ok(!result.messages.some(message => message.message.includes('Unable to attribute')));
    const complexity = Math.max(...coreResult.messages.filter(message => message.ruleId === 'complexity').map(message => Number(message.message.match(/complexity of (\d+)/)?.[1] ?? 0)));
    const depth = Math.max(...coreResult.messages.filter(message => message.ruleId === 'max-depth').map(message => Number(message.message.match(/too deeply \((\d+)\)/)?.[1] ?? 0)));
    const linesMessage = coreResult.messages.find(message => message.ruleId === 'max-lines-per-function' && message.message.includes('measured'));
    const lines = Number(linesMessage.message.match(/lines \((\d+)\)/)?.[1]);
    assert.equal(measured.complexity, complexity);
    assert.equal(measured['max-depth'], depth);
    assert.equal(measured['max-lines-per-function'], lines);
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
    const reports = result.messages.filter(message => message.ruleId === 'maintainability/metrics').map(message => JSON.parse(message.message));
    assert.ok(reports.some(item => item.function.includes('<field:field>')));
    assert.ok(reports.some(item => item.function.includes('<static#1>')), JSON.stringify(reports));
    assert.ok(!reports.some(item => item.function === 'Example.method'));
    assert.ok(!result.messages.some(message => message.message.includes('Unable to attribute')));
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

test('collects uniquely qualified function declarations and nested closures', () => {
  const loc = { start: { line: 1 }, end: { line: 5 } };
  const ast = {
    type: 'Program', loc,
    body: [{ type: 'FunctionDeclaration', id: { name: 'root' }, loc, body: { type: 'BlockStatement', loc, body: [
      { type: 'FunctionDeclaration', id: { name: 'helper' }, loc, body: { type: 'BlockStatement', loc, body: [] } },
    ] } }],
  };
  assert.deepEqual(collectFunctionNodes(ast).functions.map(item => item.name), ['root', 'root.helper']);
});

test('duplicate identities are detected and assigned deterministic unique names', () => {
  const loc = { start: { line: 1 }, end: { line: 2 } };
  const fn = () => ({ type: 'FunctionExpression', loc, params: [], body: { type: 'BlockStatement', loc, body: [] } });
  const ast = { type: 'Program', loc, body: [
    { type: 'VariableDeclaration', declarations: [{ type: 'VariableDeclarator', id: { type: 'Identifier', name: 'same' }, init: fn() }] },
    { type: 'VariableDeclaration', declarations: [{ type: 'VariableDeclarator', id: { type: 'Identifier', name: 'same' }, init: fn() }] },
  ] };
  const collected = collectFunctionNodes(ast);
  assert.deepEqual(collected.duplicateIdentities, ['same']);
  assert.deepEqual(collected.functions.map(item => item.name), ['same', 'same<duplicate#2>']);
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
