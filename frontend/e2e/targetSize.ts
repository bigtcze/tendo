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
 * audit). Pass it to `locator.evaluateAll`; it is serialized into the page, so everything it uses is defined inside it.
 *
 * The pressed box of a control is, in order:
 * 1. its enclosing `<label>`, which forwards every press to a wrapped native control;
 * 2. for a stretched link, the box its `::after` really fills: a generated (`content` is not `none`), hit-testable
 *    (`pointer-events` is not `none`), absolutely positioned pseudo-element pinned to all four edges fills its
 *    containing block. That block is the nearest ancestor that establishes one for absolute boxes (a positioned box,
 *    or one with a transform, filter, perspective, paint/layout containment or a size container); `offsetParent` is not
 *    used, because it also returns static table cells and skips transformed wrappers. A link that establishes the
 *    block itself (it is positioned or transformed) is only covering itself, and a block that is the root covers the
 *    page rather than a row, so both fall back to the control's own box;
 * 3. otherwise the control's own box.
 * Anything unrecognised falls back to the control's own box, so an unusual layout is reported, never waved through.
 */
export function measureTargets(elements: Element[]): TargetMeasurement[] {
  const containsAbsolute = (style: CSSStyleDeclaration) => style.position !== 'static'
    || style.transform !== 'none'
    || style.filter !== 'none'
    || style.perspective !== 'none'
    || (style.backdropFilter ?? 'none') !== 'none'
    || /\b(layout|paint|strict|content)\b/.test(style.contain)
    || /\b(transform|filter|perspective)\b/.test(style.willChange)
    || (style.containerType ?? 'normal') !== 'normal';
  const stretchedArea = (element: Element): Element | null => {
    const overlay = getComputedStyle(element, '::after');
    const generated = overlay.content !== 'none' && overlay.content !== 'normal' && overlay.display !== 'none';
    const pinned = [overlay.top, overlay.right, overlay.bottom, overlay.left].every((side) => side === '0px');
    if (!generated || overlay.position !== 'absolute' || overlay.pointerEvents === 'none' || !pinned) return null;
    if (containsAbsolute(getComputedStyle(element))) return null;
    for (let ancestor = element.parentElement; ancestor; ancestor = ancestor.parentElement) {
      if (ancestor === document.body || ancestor === document.documentElement) return null;
      if (containsAbsolute(getComputedStyle(ancestor))) return ancestor;
    }
    return null;
  };
  const pressedArea = (element: Element) => element.closest('label') ?? stretchedArea(element) ?? element;
  return elements.map((element) => {
    const rect = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    const pressed = pressedArea(element).getBoundingClientRect();
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
