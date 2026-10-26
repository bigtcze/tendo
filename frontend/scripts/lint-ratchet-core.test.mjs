import assert from 'node:assert/strict';
import test from 'node:test';
import { compareMaintainability, collectFunctionMetrics, metricKey } from './lint-ratchet-core.mjs';

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
  assert.match(errors.join('\\n'), /Screen.second complexity measured=21/);
  assert.deepEqual(compareMaintainability(new Map([[first, 18], [second, 21]]), [{ ...baseline[0], allowed: 18 }]).length, 1);
});

test('collects standalone metrics for each function declaration', () => {
  const ast = {
    type: 'Program',
    body: [{ type: 'FunctionDeclaration', id: { name: 'root' }, body: { loc: { start: { line: 1 }, end: { line: 5 } }, body: [
      { type: 'FunctionDeclaration', id: { name: 'helper' }, body: { loc: { start: { line: 2 }, end: { line: 4 } }, body: [] } },
    ] } }],
  };
  assert.deepEqual(collectFunctionMetrics(ast).map(item => item.function), ['root', 'root.helper']);
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
