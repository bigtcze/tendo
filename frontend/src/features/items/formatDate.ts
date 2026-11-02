function dateParts(value: string): { year: number; month: number; day: number } {
  const [year, month, day] = value.split('-').map(Number);
  return { year, month, day };
}

function dateValue({ year, month, day }: ReturnType<typeof dateParts>): Date {
  return new Date(Date.UTC(year, month - 1, day));
}

export function formatDate(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(dateValue(dateParts(value)));
}

/** Omits a redundant year only when both dates fall in the same calendar year. */
export function formatAttentionDate(value: string, evaluatedOn: string, locale: string): string {
  const date = dateParts(value);
  const reference = dateParts(evaluatedOn);
  const options: Intl.DateTimeFormatOptions = date.year === reference.year
    ? { month: 'short', day: 'numeric' }
    : { dateStyle: 'medium' };
  return new Intl.DateTimeFormat(locale, { ...options, timeZone: 'UTC' }).format(dateValue(date));
}
