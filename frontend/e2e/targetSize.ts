/** One control as a reader presses it. `pressed` is the box a press anywhere inside activates the control. */
export interface TargetMeasurement {
  tag: string;
  role: string | null;
  ariaLabel: string | null;
  text: string;
  innerText: string;
  placeholder: string | null;
  type: string | null;
  outerHTML: string;
  srOnly: boolean;
  focused: boolean;
  /** Painted at all (a non-empty box, not hidden, not transparent). */
  rendered: boolean;
  inViewport: boolean;
  box: { width: number; height: number };
  pressed: { x: number; y: number; width: number; height: number };
}

/**
 * The single WCAG 2.5.8 target-size predicate for every browser check (journey targets, route audits, the opt-in UX
 * audit). Pass it to `locator.evaluateAll`; it is serialized into the page, so everything it uses is defined inside it
 * (do not instrument or transpile it into code that calls module-level helpers).
 *
 * The pressed box of a control is, in order:
 * 1. the `<label>` that wraps it and whose `control` it is: a press anywhere on that label activates it. Other
 *    elements inside a label (a link, a second control) are measured by themselves;
 * 2. for a stretched link, the box its `::after` really fills. The pseudo-element must be generated, hit-testable
 *    (`pointer-events` and `visibility` allow presses), untransformed and unclipped, absolutely positioned and pinned
 *    to all four edges of its containing block. That block is the nearest ancestor that establishes one for absolute
 *    boxes (positioned, transformed through `transform`/`translate`/`rotate`/`scale`, filtered, contained, a size
 *    container, ...); `offsetParent` is not used, because it also returns static table cells and skips transformed
 *    wrappers. The overlay's used size must then equal that block's padding box (within 1 px), which catches margins,
 *    explicit sizes and any containing-block trigger the list misses. A link that establishes the block itself, or a
 *    block that is the page root, falls back to the control's own box;
 * 3. otherwise the control's own box.
 * Anything unrecognised falls back to the control's own box, so an unusual layout is reported, never waved through.
 * The rule may under-count (a `::before` overlay, a negative inset, a sibling control painted over part of a row); it
 * never over-counts.
 */
export function measureTargets(elements: Element[]): TargetMeasurement[] {
  const none = (value: string | undefined) => value === undefined || value === '' || value === 'none';
  const containsAbsolute = (style: CSSStyleDeclaration) => style.position !== 'static'
    || !none(style.transform) || !none(style.translate) || !none(style.rotate) || !none(style.scale)
    || !none(style.filter) || !none(style.backdropFilter) || !none(style.perspective)
    || !none(style.offsetPath) || style.transformStyle === 'preserve-3d' || style.contentVisibility === 'auto'
    || /\b(layout|paint|strict|content)\b/.test(style.contain)
    || /\b(transform|translate|rotate|scale|filter|backdrop-filter|perspective|contain|offset-path)\b/.test(style.willChange)
    || (style.containerType || 'normal') !== 'normal';
  type Box = { x: number; y: number; width: number; height: number };
  const stretchedArea = (element: Element): Box | null => {
    const overlay = getComputedStyle(element, '::after');
    const generated = !none(overlay.content) && overlay.content !== 'normal' && overlay.display !== 'none';
    const pinned = [overlay.top, overlay.right, overlay.bottom, overlay.left].every((side) => side === '0px');
    const pressable = overlay.pointerEvents !== 'none' && overlay.visibility === 'visible';
    const untransformed = [overlay.transform, overlay.translate, overlay.rotate, overlay.scale, overlay.clipPath].every(none);
    if (!generated || overlay.position !== 'absolute' || !pinned || !pressable || !untransformed) return null;
    if (containsAbsolute(getComputedStyle(element))) return null;
    for (let ancestor = element.parentElement; ancestor; ancestor = ancestor.parentElement) {
      if (ancestor === document.body || ancestor === document.documentElement) return null;
      if (!containsAbsolute(getComputedStyle(ancestor))) continue;
      const width = Number.parseFloat(overlay.width);
      const height = Number.parseFloat(overlay.height);
      if (!(Math.abs(width - ancestor.clientWidth) <= 1 && Math.abs(height - ancestor.clientHeight) <= 1)) return null;
      const block = ancestor.getBoundingClientRect();
      return { x: block.x + ancestor.clientLeft, y: block.y + ancestor.clientTop, width, height };
    }
    return null;
  };
  const ownBox = (element: Element): Box => {
    const box = element.getBoundingClientRect();
    return { x: box.x, y: box.y, width: box.width, height: box.height };
  };
  const pressedArea = (element: Element): Box => {
    const label = element.closest('label');
    if (label && label.control === element) return ownBox(label);
    return stretchedArea(element) ?? ownBox(element);
  };
  return elements.map((element) => {
    const rect = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    const pressed = pressedArea(element);
    return {
      tag: element.tagName,
      role: element.getAttribute('role'),
      ariaLabel: element.getAttribute('aria-label'),
      text: element.textContent?.trim() ?? '',
      innerText: element instanceof HTMLElement ? element.innerText : '',
      placeholder: element.getAttribute('placeholder'),
      type: element.getAttribute('type'),
      outerHTML: element.outerHTML,
      srOnly: element.matches('.sr-only'),
      focused: element.matches(':focus'),
      rendered: rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && Number(style.opacity) > 0,
      inViewport: rect.right > 0 && rect.bottom > 0 && rect.left < window.innerWidth && rect.top < window.innerHeight,
      box: { width: rect.width, height: rect.height },
      pressed: { x: pressed.x, y: pressed.y, width: pressed.width, height: pressed.height },
    };
  });
}

/** The controls the required journey checks: links, buttons, form fields and the custom switches and radios. */
export const TARGET_SELECTOR = 'a[href],button,input,select,textarea,[role="switch"],[role="radio"]';

/** Chrome lays out in 1/64 px units, so a 24 px minimum can measure 23.99998 px at some offsets. */
export const LAYOUT_UNIT = 1 / 64;

/** Whether a measured control's pressed box is under `minimum` CSS px in either direction (1/64 px tolerance). */
export function pressedBelow(target: TargetMeasurement, minimum: number): boolean {
  return target.pressed.width < minimum - LAYOUT_UNIT || target.pressed.height < minimum - LAYOUT_UNIT;
}
