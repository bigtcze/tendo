import { describe, expect, it } from 'vitest';
import { codePointLength } from './textLength';

describe('codePointLength', () => {
  it('counts code points, not UTF-16 code units', () => {
    expect(codePointLength('')).toBe(0);
    expect(codePointLength('Café')).toBe(4);
    expect('😀'.length).toBe(2);
    expect(codePointLength('😀')).toBe(1);
    expect(codePointLength('a😀b')).toBe(3);
    // Combining sequences are several code points, as the server counts them.
    expect(codePointLength('e\u0301')).toBe(2);
    expect(codePointLength('\uD83D')).toBe(1);
  });
});
