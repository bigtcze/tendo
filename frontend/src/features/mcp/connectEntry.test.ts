import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  clearConnectEntry, markConnectReturn, parseConnectFragment, readConnectEntry, saveConnectHandle,
  setConnectClockForTests, takeConnectReturn,
} from './connectEntry';

const handle = 'A'.repeat(43);
const STORAGE_KEY = 'tendo.mcpConnect';
const T0 = 1_800_000_000_000;
let now = T0;

beforeEach(() => {
  sessionStorage.clear();
  now = T0;
  setConnectClockForTests(() => now);
  clearConnectEntry();
});
afterEach(() => {
  setConnectClockForTests(null);
  vi.restoreAllMocks();
});

describe('parseConnectFragment', () => {
  it('returns null for an empty fragment', () => {
    expect(parseConnectFragment('')).toBeNull();
    expect(parseConnectFragment('#')).toBeNull();
  });

  it('accepts exactly one request handle of 43 base64url characters', () => {
    expect(parseConnectFragment(`#request=${handle}`)).toEqual({ kind: 'request', handle });
    expect(parseConnectFragment(`request=${'a-_9'.repeat(10)}a-_`)).toEqual({ kind: 'request', handle: `${'a-_9'.repeat(10)}a-_` });
  });

  it.each([
    ['42 characters', `#request=${'A'.repeat(42)}`],
    ['44 characters', `#request=${'A'.repeat(44)}`],
    ['a disallowed character', `#request=${'A'.repeat(42)}+`],
    ['padding', `#request=${'A'.repeat(42)}=`],
    ['an empty value', '#request='],
  ])('rejects a handle with %s', (_name, hash) => {
    expect(parseConnectFragment(hash)).toEqual({ kind: 'invalid' });
  });

  it('rejects duplicate, extra and mixed keys', () => {
    expect(parseConnectFragment(`#request=${handle}&request=${handle}`)).toEqual({ kind: 'invalid' });
    expect(parseConnectFragment(`#request=${handle}&extra=1`)).toEqual({ kind: 'invalid' });
    expect(parseConnectFragment(`#request=${handle}&error=server_error`)).toEqual({ kind: 'invalid' });
  });

  it('accepts a known error code and rejects an unknown one', () => {
    expect(parseConnectFragment('#error=invalid_scope')).toEqual({ kind: 'error', code: 'invalid_scope' });
    expect(parseConnectFragment('#error=access_denied')).toEqual({ kind: 'invalid' });
    expect(parseConnectFragment('#error=<script>')).toEqual({ kind: 'invalid' });
  });

  it('never returns raw fragment text for an unknown key', () => {
    expect(parseConnectFragment('#state=secret-value')).toEqual({ kind: 'invalid' });
  });
});

describe('stored consent entry', () => {
  it('saves a handle with the return flag false and reads it back while fresh', () => {
    saveConnectHandle(handle);
    expect(readConnectEntry()).toEqual({ handle, savedAt: T0, returnAfterSignIn: false });
  });

  it('a new fragment replaces the entry and resets the flag', () => {
    saveConnectHandle(handle);
    markConnectReturn();
    const other = 'B'.repeat(43);
    saveConnectHandle(other);
    expect(readConnectEntry()).toEqual({ handle: other, savedAt: T0, returnAfterSignIn: false });
  });

  it('expires after five minutes and tolerates five seconds of clock skew into the future', () => {
    saveConnectHandle(handle);
    now = T0 + 5 * 60 * 1000;
    expect(readConnectEntry()).not.toBeNull();
    now = T0 + 5 * 60 * 1000 + 1;
    expect(readConnectEntry()).toBeNull();

    // A record stamped 5 s ahead of the reader is still accepted; 6 s ahead is refused.
    now = T0 + 5_000;
    saveConnectHandle(handle);
    now = T0;
    expect(readConnectEntry()).not.toBeNull();
    now = T0 + 6_000;
    saveConnectHandle(handle);
    now = T0;
    expect(readConnectEntry()).toBeNull();
  });

  it.each([
    ['an extra key', { handle, savedAt: T0, returnAfterSignIn: false, extra: 1 }],
    ['a missing key', { handle, savedAt: T0 }],
    ['a malformed handle', { handle: 'short', savedAt: T0, returnAfterSignIn: false }],
    ['a non-numeric savedAt', { handle, savedAt: '1', returnAfterSignIn: false }],
    ['a future savedAt', { handle, savedAt: T0 + 60_000, returnAfterSignIn: false }],
    ['a non-boolean flag', { handle, savedAt: T0, returnAfterSignIn: 'yes' }],
    ['an array', [handle]],
  ])('removes a tampered record with %s', (_name, value) => {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(value));
    expect(readConnectEntry()).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('removes corrupt JSON', () => {
    sessionStorage.setItem(STORAGE_KEY, '{not json');
    expect(readConnectEntry()).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('treats an expired stored record as absent', () => {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ handle, savedAt: T0 - 6 * 60 * 1000, returnAfterSignIn: false }));
    expect(readConnectEntry()).toBeNull();
  });

  it('clears the entry', () => {
    saveConnectHandle(handle);
    clearConnectEntry();
    expect(readConnectEntry()).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});

describe('takeConnectReturn', () => {
  it('is false without a marked entry', () => {
    saveConnectHandle(handle);
    expect(takeConnectReturn()).toBe(false);
  });

  it('is true once after markConnectReturn and keeps the handle', () => {
    saveConnectHandle(handle);
    markConnectReturn();
    expect(takeConnectReturn()).toBe(true);
    expect(takeConnectReturn()).toBe(false);
    expect(readConnectEntry()).toEqual({ handle, savedAt: T0, returnAfterSignIn: false });
  });

  it('markConnectReturn is a no-op without a fresh entry', () => {
    markConnectReturn();
    expect(takeConnectReturn()).toBe(false);
    expect(readConnectEntry()).toBeNull();
  });
});

describe('storage failure', () => {
  it('keeps the entry in memory when writing to storage throws', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
    saveConnectHandle(handle);
    markConnectReturn();
    expect(readConnectEntry()).toEqual({ handle, savedAt: T0, returnAfterSignIn: true });
    expect(takeConnectReturn()).toBe(true);
  });

  it('keeps memory authoritative over a stale stored copy', () => {
    saveConnectHandle(handle);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
    const other = 'B'.repeat(43);
    saveConnectHandle(other);
    expect(readConnectEntry()?.handle).toBe(other);
  });

  it('answers from memory when reading storage throws', () => {
    saveConnectHandle(handle);
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied'); });
    expect(readConnectEntry()?.handle).toBe(handle);
  });
});
