/** Whether computed focus styles paint a visible outline or shadow. Serializable for page.evaluate. */
export function hasPaintedFocusIndicator(element: Element): boolean {
  const style = getComputedStyle(element);
  const visibleColor = (value: string) => {
    if (value.includes('transparent')) return false;
    const match = value.match(/rgba?\(([^)]*)\)/);
    if (!match) return true;
    const components = match[1]!.split(/[,/ ]+/).filter(Boolean);
    const alpha = components.length >= 4 ? Number.parseFloat(components[3]!) : 1;
    return Number.isFinite(alpha) && alpha > 0;
  };
  const outlinePainted = style.outlineStyle !== 'none'
    && Number.parseFloat(style.outlineWidth) > 0
    && visibleColor(style.outlineColor);
  return outlinePainted || (style.boxShadow !== 'none' && visibleColor(style.boxShadow));
}

/** Computed outline + shadow, for comparing an element's focused and unfocused paint. Serializable for page.evaluate. */
export function focusIndicatorSignature(element: Element): string {
  const style = getComputedStyle(element);
  return `${style.outlineStyle}/${style.outlineWidth}/${style.outlineColor}/${style.outlineOffset}/${style.boxShadow}`;
}
