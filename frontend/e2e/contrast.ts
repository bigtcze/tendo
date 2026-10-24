export type Color = { r: number; g: number; b: number; a: number };

export type ContrastKind = 'text' | 'placeholder' | 'boundary' | 'native-control';

export type ContrastCandidate = {
  kind: ContrastKind;
  name: string;
  foreground: string;
  background: Color;
  fontSize: number;
  fontWeight: number;
  disabled: boolean;
  exempt?: boolean;
  note?: string;
};

export type ContrastResult = {
  kind: ContrastKind;
  name: string;
  foreground: string;
  background: string;
  ratio: number | null;
  threshold: number;
  exempt: boolean;
  note?: string;
};

/** Parses CSS computed colors emitted by Chromium (rgb/rgba, modern srgb, and OKLCH). */
export function parseColor(value: string): Color | null {
  const input = value.trim().toLowerCase();
  const match = input.match(/^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)(?:\s*[,/]\s*([\d.]+)(%)?)?\s*\)$/);
  if (match) return { r: Number(match[1]), g: Number(match[2]), b: Number(match[3]), a: match[4] === undefined ? 1 : Number(match[4]) / (match[5] ? 100 : 1) };
  const srgb = input.match(/^color\(\s*srgb\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s*\/\s*([\d.]+))?\s*\)$/);
  if (srgb) return { r: Number(srgb[1]) * 255, g: Number(srgb[2]) * 255, b: Number(srgb[3]) * 255, a: srgb[4] === undefined ? 1 : Number(srgb[4]) };
  const oklch = input.match(/^oklch\(\s*([\d.]+)(%)?\s+([\d.]+)\s+([\d.]+)(?:\s*\/\s*([\d.]+))?\s*\)$/);
  if (!oklch) return null;
  const lightness = Number(oklch[1]) / (oklch[2] ? 100 : 1);
  const chroma = Number(oklch[3]);
  const hue = (Number(oklch[4]) * Math.PI) / 180;
  const aAxis = chroma * Math.cos(hue);
  const bAxis = chroma * Math.sin(hue);
  const l = (lightness + 0.3963377774 * aAxis + 0.2158037573 * bAxis) ** 3;
  const m = (lightness - 0.1055613458 * aAxis - 0.0638541728 * bAxis) ** 3;
  const s = (lightness - 0.0894841775 * aAxis - 1.291485548 * bAxis) ** 3;
  const linearRgb = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  const encodeSrgb = (value: number) =>
    255 * (value <= 0.0031308 ? 12.92 * value : 1.055 * Math.max(0, value) ** (1 / 2.4) - 0.055);
  return {
    r: encodeSrgb(linearRgb[0]),
    g: encodeSrgb(linearRgb[1]),
    b: encodeSrgb(linearRgb[2]),
    a: oklch[5] === undefined ? 1 : Number(oklch[5]),
  };
}

export function composite(foreground: Color, backdrop: Color): Color {
  const alpha = foreground.a + backdrop.a * (1 - foreground.a);
  if (!alpha) return { r: 0, g: 0, b: 0, a: 0 };
  return {
    r: (foreground.r * foreground.a + backdrop.r * backdrop.a * (1 - foreground.a)) / alpha,
    g: (foreground.g * foreground.a + backdrop.g * backdrop.a * (1 - foreground.a)) / alpha,
    b: (foreground.b * foreground.a + backdrop.b * backdrop.a * (1 - foreground.a)) / alpha,
    a: alpha,
  };
}

export function relativeLuminance(color: Color): number {
  const linear = (value: number) => {
    const channel = value / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * linear(color.r) + 0.7152 * linear(color.g) + 0.0722 * linear(color.b);
}

export function contrastRatio(a: Color, b: Color): number {
  const [light, dark] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}

export function passesContrastThreshold(ratio: number, threshold: number): boolean {
  return ratio >= threshold;
}

export function isLargeText(fontSize: number, fontWeight: number): boolean {
  return fontSize >= 24 || (fontSize >= 18.66 && fontWeight >= 700);
}

export function boundaryPasses(boundary: Color, outside: Color, ownBackground: Color): boolean {
  return contrastRatio(boundary, outside) >= 3 || contrastRatio(ownBackground, outside) >= 3;
}

export function evaluateContrast(candidate: ContrastCandidate): ContrastResult {
  const exempt = candidate.disabled || candidate.exempt === true;
  const threshold = candidate.kind === 'text' ? (isLargeText(candidate.fontSize, candidate.fontWeight) ? 3 : 4.5) : candidate.kind === 'placeholder' ? 4.5 : 3;
  const foreground = parseColor(candidate.foreground);
  const background = candidate.background;
  if (candidate.kind === 'native-control') return { kind: candidate.kind, name: candidate.name, foreground: candidate.foreground, background: colorString(background), ratio: null, threshold, exempt, note: candidate.note ?? 'native, unmeasured' };
  if (!foreground) throw new Error(`Unsupported foreground color ${candidate.foreground} (${candidate.name})`);
  const ratio = contrastRatio(foreground, background);
  return { kind: candidate.kind, name: candidate.name, foreground: candidate.foreground, background: colorString(background), ratio, threshold, exempt, note: candidate.note };
}

export function colorString(color: Color): string {
  return `rgb(${Math.round(color.r)} ${Math.round(color.g)} ${Math.round(color.b)})`;
}
