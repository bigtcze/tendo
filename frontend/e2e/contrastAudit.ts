import { boundaryPasses, composite, contrastRatio, evaluateContrast, parseColor, type Color, type ContrastResult } from './contrast';
import type { Page } from '@playwright/test';

type RawCandidate = {
  kind: 'text' | 'placeholder' | 'boundary' | 'native-control';
  name: string;
  foreground: string;
  background: string;
  outside: string;
  ownBackground: string;
  fontSize: number;
  fontWeight: number;
  disabled: boolean;
  exempt: boolean;
  borderWidth: number;
  opacity: number;
  native: boolean;
};

export type ContrastAuditRow = ContrastResult & { route: string };

const TRANSPARENT = 'rgba(0, 0, 0, 0)';
const PAPER = 'rgb(250, 247, 242)';

/** Collect rendered text and control candidates using the browser's resolved styles. */
export async function collectContrastCandidates(page: Page): Promise<RawCandidate[]> {
  // Entry animations fade content in from opacity 0; measuring mid-animation reports transient, not rendered, colours.
  await page.waitForFunction(() => document.getAnimations().every((animation) => animation.playState !== 'running'));
  return page.evaluate(({ paper, transparent }) => {
    // Chromium serializes color-mix() (e.g. Tailwind bg-white/70) as oklab(); let the canvas convert any CSS colour to sRGB.
    // Fail closed: an unreadable colour must never be skipped as if it were transparent.
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error('2D canvas unavailable for colour normalization');
    const parse = (value: string): Color => {
      if (!CSS.supports('color', value)) throw new Error(`unsupported colour ${value}`);
      context.clearRect(0, 0, 1, 1);
      context.fillStyle = value;
      context.fillRect(0, 0, 1, 1);
      const [r, g, b, alpha] = context.getImageData(0, 0, 1, 1).data;
      return { r, g, b, a: alpha / 255 };
    };
    const formatColor = (color: Color) => `rgba(${color.r}, ${color.g}, ${color.b}, ${color.a})`;
    const formatRgb = (color: Color) => `rgb(${Math.round(color.r)} ${Math.round(color.g)} ${Math.round(color.b)})`;
    const blend = (foreground: Color, backdrop: Color): Color => {
      const alpha = foreground.a + backdrop.a * (1 - foreground.a);
      if (!alpha) return { r: 0, g: 0, b: 0, a: 0 };
      return {
        r: (foreground.r * foreground.a + backdrop.r * backdrop.a * (1 - foreground.a)) / alpha,
        g: (foreground.g * foreground.a + backdrop.g * backdrop.a * (1 - foreground.a)) / alpha,
        b: (foreground.b * foreground.a + backdrop.b * backdrop.a * (1 - foreground.a)) / alpha,
        a: alpha,
      };
    };
    const isVisible = (element: Element) => {
      const style = getComputedStyle(element);
      const rect = element.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0 || style.display === 'none' || style.visibility === 'hidden') return false;
      for (let node: Element | null = element; node; node = node.parentElement) {
        const current = getComputedStyle(node);
        if (Number(current.opacity) === 0) return false;
        const currentRect = node.getBoundingClientRect();
        const onePixel = currentRect.width <= 1 && currentRect.height <= 1;
        const clippedToNothing = current.clip === 'rect(0px, 0px, 0px, 0px)' || current.clipPath === 'inset(50%)';
        if (onePixel && clippedToNothing) return false;
      }
      return true;
    };
    const disabled = (element: Element) => element.matches(':disabled') || Boolean(element.closest('[disabled],[aria-disabled="true"]'));
    const describe = (element: Element) => `${element.tagName.toLowerCase()}${element.id ? `#${element.id}` : ''}${[...element.classList].slice(0, 2).map((name) => `.${name}`).join('')}`;
    const layers = (element: Element) => {
      const chain: Element[] = [];
      for (let node: Element | null = element; node; node = node.parentElement) chain.push(node);
      let background = parse(paper);
      for (const node of chain.reverse()) {
        const style = getComputedStyle(node);
        if (style.backgroundImage !== 'none') throw new Error(`background image is unmeasurable at ${describe(element)}: ${style.backgroundImage}`);
        background = blend(parse(style.backgroundColor), background);
      }
      return background;
    };
    const ownColor = (element: Element) => parse(getComputedStyle(element).backgroundColor);
    const outsideColor = (element: Element) => element.parentElement ? layers(element.parentElement) : parse(paper);
    const result: RawCandidate[] = [];
    const textElements = new Set<Element>();
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (node.textContent?.trim() && node.parentElement) textElements.add(node.parentElement);
    }
    for (const element of textElements) {
      if (element instanceof HTMLOptionElement || element.closest('select')) continue;
      if (!isVisible(element)) continue;
      const style = getComputedStyle(element);
      let opacity = 1;
      for (let node: Element | null = element; node; node = node.parentElement) opacity *= Number(getComputedStyle(node).opacity || 1);
      const foreground = parse(style.color);
      const background = layers(element);
      const composited = foreground ? blend({ ...foreground, a: foreground.a * opacity }, background) : null;
      result.push({
        kind: 'text',
        name: describe(element),
        foreground: composited ? `rgba(${composited.r}, ${composited.g}, ${composited.b}, 1)` : style.color,
        background: formatRgb(background),
        outside: formatRgb(outsideColor(element)),
        ownBackground: formatColor(ownColor(element)),
        fontSize: parseFloat(style.fontSize),
        fontWeight: Number(style.fontWeight),
        disabled: disabled(element),
        exempt: false,
        borderWidth: 0,
        opacity,
        native: false,
      });
    }
    for (const element of document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input[placeholder],textarea[placeholder]')) {
      if (!isVisible(element)) continue;
      const placeholder = getComputedStyle(element, '::placeholder');
      const inputBackground = layers(element);
      const fg = parse(placeholder.color);
      let opacity = Number(placeholder.opacity || 1);
      for (let node: Element | null = element; node; node = node.parentElement) opacity *= Number(getComputedStyle(node).opacity || 1);
      const composited = fg ? blend({ ...fg, a: fg.a * opacity }, inputBackground) : null;
      result.push({
        kind: 'placeholder',
        name: `${describe(element)}[placeholder]`,
        foreground: composited ? `rgba(${composited.r}, ${composited.g}, ${composited.b}, 1)` : placeholder.color,
        background: formatRgb(inputBackground),
        outside: formatRgb(outsideColor(element)),
        ownBackground: formatColor(ownColor(element)),
        fontSize: parseFloat(placeholder.fontSize),
        fontWeight: Number(placeholder.fontWeight),
        disabled: disabled(element),
        exempt: false,
        borderWidth: 0,
        opacity,
        native: false,
      });
    }
    const valueControls = document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(
      'input:not([type="button"]):not([type="submit"]):not([type="reset"]):not([type="checkbox"]):not([type="radio"]):not([type="hidden"]):not([type="file"]):not([type="image"]):not([type="color"]):not([type="range"]),textarea,select',
    );
    for (const element of valueControls) {
      const displayedText = element instanceof HTMLSelectElement
        ? element.selectedOptions[0]?.text.trim() ?? ''
        : element.value.trim();
      if (!displayedText || !isVisible(element)) continue;
      const style = getComputedStyle(element);
      const background = layers(element);
      let opacity = 1;
      for (let node: Element | null = element; node; node = node.parentElement) {
        opacity *= Number(getComputedStyle(node).opacity || 1);
      }
      const foreground = parse(style.color);
      const composited = foreground ? blend({ ...foreground, a: foreground.a * opacity }, background) : null;
      result.push({
        kind: 'text',
        name: `${describe(element)}[value]`,
        foreground: composited ? `rgba(${composited.r}, ${composited.g}, ${composited.b}, 1)` : style.color,
        background: formatRgb(background),
        outside: formatRgb(outsideColor(element)),
        ownBackground: formatColor(ownColor(element)),
        fontSize: parseFloat(style.fontSize),
        fontWeight: Number(style.fontWeight),
        disabled: disabled(element),
        exempt: false,
        borderWidth: 0,
        opacity,
        native: false,
      });
    }
    const selector = 'input,select,textarea,button,[role="switch"],[role="checkbox"],[role="radio"]';
    for (const element of document.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(selector)) {
      if (!isVisible(element)) continue;
      const style = getComputedStyle(element);
      const isButton = element instanceof HTMLButtonElement || (element instanceof HTMLInputElement && ['button', 'submit', 'reset'].includes(element.type));
      const hasTextLabel = Boolean(element.textContent?.trim() || (element instanceof HTMLInputElement && element.value.trim()));
      if (isButton && hasTextLabel) continue;
      const isNative = element instanceof HTMLInputElement && ['checkbox', 'radio'].includes(element.type) && style.appearance === 'auto';
      const border = parse(style.borderTopColor);
      const background = layers(element);
      result.push({
        kind: isNative ? 'native-control' : 'boundary',
        name: element.getAttribute('aria-label') || describe(element),
        foreground: border ? `rgba(${border.r}, ${border.g}, ${border.b}, ${border.a})` : style.borderTopColor,
        background: formatRgb(background),
        outside: formatRgb(outsideColor(element)),
        ownBackground: formatColor(ownColor(element)),
        fontSize: 0,
        fontWeight: 400,
        disabled: disabled(element),
        exempt: false,
        borderWidth: parseFloat(style.borderTopWidth),
        opacity: 1,
        native: isNative,
      });
    }
    return result.map((candidate) => ({ ...candidate, background: candidate.background || transparent }));
  }, { paper: PAPER, transparent: TRANSPARENT });
}

/** Apply WCAG thresholds and boundary-indicator exception to browser-collected candidates. */
export async function auditContrast(page: Page, route: string): Promise<ContrastAuditRow[]> {
  const candidates = await collectContrastCandidates(page);
  return candidates.map((candidate) => {
    const background = parseColor(candidate.kind === 'boundary' ? candidate.outside : candidate.background)!;
    const result = evaluateContrast({
      kind: candidate.kind,
      name: candidate.name,
      foreground: candidate.foreground,
      background,
      fontSize: candidate.fontSize,
      fontWeight: candidate.fontWeight,
      disabled: candidate.disabled,
      exempt: candidate.exempt,
      note: candidate.native ? 'native, unmeasured' : undefined,
    });
    if (candidate.kind === 'boundary') {
      const outside = parseColor(candidate.outside)!;
      const own = parseColor(candidate.ownBackground)!;
      const border = parseColor(candidate.foreground)!;
      const effectiveBorder = composite(border, outside);
      const effectiveOwn = composite(own, outside);
      const ownContrast = contrastRatio(effectiveOwn, outside);
      const passes = candidate.borderWidth > 0
        ? boundaryPasses(effectiveBorder, outside, effectiveOwn)
        : ownContrast >= 3;
      return {
        ...result,
        route,
        ratio: candidate.borderWidth > 0 ? contrastRatio(effectiveBorder, outside) : ownContrast,
        exempt: result.exempt || passes,
        note: candidate.borderWidth > 0 ? result.note : 'no border indicator',
      };
    }
    return { ...result, route };
  });
}
