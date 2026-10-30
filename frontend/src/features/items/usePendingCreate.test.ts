import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { clearPendingItemCreates, getPendingItemCreate, setPendingItemCreate } from './pendingItemCreate';
import { usePendingCreate } from './usePendingCreate';

const original = { version: 1 as const, key: 'aa'.repeat(16), body: { title: 'Original', subjectId: 'subject' } };
const next = { version: 1 as const, key: 'bb'.repeat(16), body: { title: 'Next', subjectId: 'subject' } };

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

afterEach(() => clearPendingItemCreates());

describe('usePendingCreate settlement', () => {
  it.each(['created', 'rejected'] as const)('preserves a newer attempt after an obsolete %s response', async (outcome) => {
    setPendingItemCreate('user', 'home', original);
    const response = deferred<void>();
    const obsolete = renderHook(() => usePendingCreate('user', 'home'));
    const settle = outcome === 'created'
      ? () => obsolete.result.current.created(original.key)
      : () => obsolete.result.current.rejected(original.key, false);
    obsolete.unmount();

    renderHook(() => usePendingCreate('user', 'home'));
    setPendingItemCreate('user', 'home', next);
    const responseHandler = response.promise.then(() => settle());
    response.resolve();
    await responseHandler;

    expect(getPendingItemCreate('user', 'home')).toEqual(next);
  });
});
