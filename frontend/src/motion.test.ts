import { describe, expect, it } from 'vitest';
import { longestCssTimeSum, parseCssTime } from '../e2e/motion';

describe('CSS motion time parsing', () => {
  it('parses seconds, milliseconds, and fractional milliseconds', () => {
    expect(parseCssTime('0.01ms')).toBe(0.01);
    expect(parseCssTime('2s')).toBe(2000);
    expect(parseCssTime('0s')).toBe(0);
  });

  it('pairs comma lists and repeats the shorter list per CSS list matching', () => {
    expect(longestCssTimeSum('400ms, 2s', '0s')).toBe(2000);
    expect(longestCssTimeSum('0.01ms, 0s', '0s, 0.02ms')).toBe(0.02);
    expect(longestCssTimeSum('100ms', '50ms, 200ms')).toBe(300);
  });

  it('rejects values it cannot read instead of counting them as no motion', () => {
    expect(() => parseCssTime('')).toThrow(/unparseable/);
    expect(() => parseCssTime('400')).toThrow(/unparseable/);
    expect(() => parseCssTime('auto')).toThrow(/unparseable/);
    expect(() => longestCssTimeSum('0s, bogus', '0s')).toThrow(/unparseable/);
    expect(parseCssTime('1e-05s')).toBeCloseTo(0.01);
  });

  it('treats infinite time as infinite', () => {
    expect(parseCssTime('infinite')).toBe(Number.POSITIVE_INFINITY);
    expect(longestCssTimeSum('infinite', '0s')).toBe(Number.POSITIVE_INFINITY);
  });
});
