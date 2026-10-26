import assert from 'node:assert/strict';
import test from 'node:test';
import { compareMaintainability } from './lint-ratchet-core.mjs';

const entry = (allowed = 20, reason = 'technical exception') => ({
  file: 'src/example.ts', rule: 'complexity', allowed, reason, owner: 'frontend-app',
});
const observed = (value) => [['src/example.ts\0complexity', value]];

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
