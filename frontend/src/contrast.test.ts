import { describe, expect, it } from 'vitest';
import {
  boundaryPasses,
  composite,
  contrastRatio,
  evaluateContrast,
  isLargeText,
  parseColor,
  type Color,
} from '../e2e/contrast';

const color = (r: number, g: number, b: number, a = 1): Color => ({ r, g, b, a });

describe('contrast calculations', () => {
  it('returns 21:1 for black and white', () => expect(contrastRatio(color(0, 0, 0), color(255, 255, 255))).toBeCloseTo(21, 10));
  it('returns the known ink/paper ratio', () => expect(contrastRatio(color(37, 48, 43), color(250, 247, 242))).toBeCloseTo(12.79, 2));
  it('checks the 4.5:1 threshold boundary', () => expect(contrastRatio(color(118, 118, 118), color(255, 255, 255))).toBeCloseTo(4.54, 1));
  it('alpha-composites over opaque backdrop', () => expect(composite(color(0, 0, 0, 0.5), color(255, 255, 255))).toEqual(color(127.5, 127.5, 127.5)));
  it.each(['rgb(1, 2, 3)', 'rgba(1, 2, 3, 0.5)', 'rgb(1 2 3 / 50%)', 'color(srgb 1 0.5 0 / 0.25)', 'oklch(50% 0 0)'])('parses %s', (value) => expect(parseColor(value)).not.toBeNull());
  it('rejects unsupported color expressions', () => expect(parseColor('linear-gradient(red, blue)')).toBeNull());
});

describe('WCAG contrast evaluation', () => {
  it('classifies large text at either size/weight boundary', () => {
    expect(isLargeText(24, 400)).toBe(true);
    expect(isLargeText(18.66, 700)).toBe(true);
    expect(isLargeText(18.66, 600)).toBe(false);
    expect(isLargeText(23.99, 400)).toBe(false);
  });

  it('exempts disabled text and reports it as exempt', () => {
    const result = evaluateContrast({ kind: 'text', name: 'disabled label', foreground: 'rgb(200, 200, 200)', background: color(255, 255, 255), fontSize: 16, fontWeight: 400, disabled: true });
    expect(result.exempt).toBe(true);
    expect(result.threshold).toBe(4.5);
  });

  it('applies the non-text boundary indicator exception', () => {
    expect(boundaryPasses(color(216, 208, 195), color(255, 255, 255), color(0, 0, 0))).toBe(true);
    expect(boundaryPasses(color(216, 208, 195), color(255, 255, 255), color(250, 247, 242))).toBe(false);
  });
});
