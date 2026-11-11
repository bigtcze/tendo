function dateParts(value: string): { year: number; month: number; day: number } {
  const [year, month, day] = value.split('-').map(Number);
  return { year, month, day };
}

function dateValue({ year, month, day }: ReturnType<typeof dateParts>): Date {
  return new Date(Date.UTC(year, month - 1, day));
}

/**
 * Joins the parts of a formatted date with U+00A0 so the date can never break across two lines inside a
 * sentence or a list row ("14 May 2026", "14. 5. 2026", "May 10"). The words around it still wrap normally,
 * and assistive technology reads the same text: the no-break space is whitespace, not punctuation.
 */
export function nonBreaking(value: string): string {
  return value.replace(/\s/g, '\u00a0');
}

export function formatDate(value: string, locale: string): string {
  return nonBreaking(new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(dateValue(dateParts(value))));
}

/** Omits a redundant year only when both dates fall in the same calendar year. */
export function formatAttentionDate(value: string, evaluatedOn: string, locale: string): string {
  const date = dateParts(value);
  const reference = dateParts(evaluatedOn);
  const options: Intl.DateTimeFormatOptions = date.year === reference.year
    ? { month: 'short', day: 'numeric' }
    : { dateStyle: 'medium' };
  return nonBreaking(new Intl.DateTimeFormat(locale, { ...options, timeZone: 'UTC' }).format(dateValue(date)));
}
