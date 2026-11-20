import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { clearConnectEntry, readConnectEntry, saveConnectHandle } from '../features/mcp/connectEntry';
import { useConnectVisit } from './useConnectVisit';

const handle = 'A'.repeat(43);
const other = 'B'.repeat(43);

beforeEach(() => {
  sessionStorage.clear();
  clearConnectEntry();
  window.history.replaceState(null, '', '/connect');
});

afterEach(() => {
  clearConnectEntry();
});

describe('useConnectVisit', () => {
  it('a hashchange to a new valid handle gives a new visit, strips the fragment and replaces the stored entry', () => {
    saveConnectHandle(handle);
    const { result } = renderHook(() => useConnectVisit());
    const first = result.current.visit;
    window.history.replaceState(null, '', `/connect#request=${other}`);
    act(() => { window.dispatchEvent(new HashChangeEvent('hashchange')); });
    expect(result.current.visit).toBe(first + 1);
    expect(result.current.outcome).toEqual({ kind: 'request', handle: other });
    expect(window.location.hash).toBe('');
    expect(readConnectEntry()?.handle).toBe(other);
  });

  it('a hashchange to mixed keys gives invalid and clears the entry', () => {
    saveConnectHandle(handle);
    const { result } = renderHook(() => useConnectVisit());
    window.history.replaceState(null, '', `/connect#request=${other}&error=invalid_client`);
    act(() => { window.dispatchEvent(new HashChangeEvent('hashchange')); });
    expect(result.current.outcome).toEqual({ kind: 'invalid' });
    expect(window.location.hash).toBe('');
    expect(readConnectEntry()).toBeNull();
  });

  it('a reload with no fragment and a fresh stored entry gives the stored request', () => {
    saveConnectHandle(handle);
    window.history.replaceState(null, '', '/connect');
    const { result } = renderHook(() => useConnectVisit());
    expect(result.current.outcome).toEqual({ kind: 'request', handle });
  });

  it('a reload with no fragment and no entry gives none', () => {
    const { result } = renderHook(() => useConnectVisit());
    expect(result.current.outcome).toEqual({ kind: 'none' });
  });
});
