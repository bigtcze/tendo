import { describe, expect, it } from 'vitest';
import { formatAttentionDate, formatDate, nonBreaking } from './formatDate';

const NBSP = '\u00a0';

describe('formatAttentionDate', () => {
  it('omits the year within the server evaluation year', () => {
    expect(formatAttentionDate('2026-03-01', '2026-01-15', 'en')).toBe(`Mar${NBSP}1`);
    expect(formatAttentionDate('2026-03-01', '2026-01-15', 'cs')).toBe(`1.${NBSP}3.`);
  });

  it('retains the year across the household year boundary', () => {
    expect(formatAttentionDate('2027-01-01', '2026-12-31', 'en')).toBe(`Jan${NBSP}1,${NBSP}2027`);
    expect(formatAttentionDate('2027-01-01', '2026-12-31', 'cs')).toBe(`1.${NBSP}1.${NBSP}2027`);
  });
});

describe('non-breaking dates', () => {
  // A date that splits across two lines ("14 May" / "2026") is a real defect in rows and the Members list.
  it('joins every part of a formatted date with a no-break space', () => {
    expect(formatDate('2026-05-14', 'en')).toBe(`May${NBSP}14,${NBSP}2026`);
    expect(formatDate('2026-05-14', 'cs')).toBe(`14.${NBSP}5.${NBSP}2026`);
    expect(formatDate('2026-05-14', 'en')).not.toContain(' ');
    expect(formatDate('2026-05-14', 'cs')).not.toContain(' ');
  });

  it('leaves already single words alone', () => {
    expect(nonBreaking('14 May 2026')).toBe(`14${NBSP}May${NBSP}2026`);
    expect(nonBreaking('2026')).toBe('2026');
  });
});
