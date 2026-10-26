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
    const branches = Array.from({ length: 16 }, (_, index) => `  if (value && value?.valueOf() === ${index}) return value ? (value ?? false) : false;`).join('\n');
    await writeFile(path, `function measured(value = true) {\n${branches}\n  return value || value?.valueOf();\n}\n`);
    const eslint = new ESLint({ cwd: directory, overrideConfigFile: true, overrideConfig: [{
      files: ['src/**/*.ts'],
      languageOptions: { parser: tseslint.parser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
      plugins: { maintainability: maintainabilityPlugin },
      rules: { 'maintainability/metrics': 'error' },
    }] });
    const [result] = await eslint.lintFiles([path]);
    const report = result.messages.find(message => message.ruleId === 'maintainability/metrics');
    assert.ok(report);
    const payload = JSON.parse(report.message);
    assert.equal(payload.function, 'measured');
    assert.ok(payload.complexity > 1, 'ternary and logical/nullish operators count in core complexity');
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
    await writeFile(join(directory, 'frontend/maintainability-baseline.json'), '{bad');
    git('add', 'frontend/maintainability-baseline.json');
    git('commit', '-qm', 'malformed baseline');
    assert.throws(() => loadBaseBaseline('HEAD', directory), /malformed baseline/);
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
