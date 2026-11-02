import { describe, expect, it } from 'vitest';
import { formatAttentionDate } from './formatDate';

describe('formatAttentionDate', () => {
  it('omits the year within the server evaluation year', () => {
    expect(formatAttentionDate('2026-03-01', '2026-01-15', 'en')).toBe('Mar 1');
    expect(formatAttentionDate('2026-03-01', '2026-01-15', 'cs')).toBe('1. 3.');
  });

  it('retains the year across the household year boundary', () => {
    expect(formatAttentionDate('2027-01-01', '2026-12-31', 'en')).toBe('Jan 1, 2027');
    expect(formatAttentionDate('2027-01-01', '2026-12-31', 'cs')).toBe('1. 1. 2027');
  });
});
