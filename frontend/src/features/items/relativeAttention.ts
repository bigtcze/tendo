export function relativeAttention(date: string, locale: string, now?: Date): string {
  const target = new Date(`${date}T00:00:00Z`);
  const localToday = now ?? new Date();
  const today = new Date(Date.UTC(localToday.getUTCFullYear(), localToday.getUTCMonth(), localToday.getUTCDate()));
  const days = Math.round((target.getTime() - today.getTime()) / 86_400_000);
  if (days === 0) return locale === 'cs' ? 'dnes' : 'today';
  const day = target.getUTCDate();
  const month = new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-GB', { month: 'short', timeZone: 'UTC' }).format(target).replace('.', '');
  if (days < 0) return locale === 'cs' ? `od ${day}. ${month}` : `since ${day} ${month}`;
  return locale === 'cs' ? `za ${days} dní` : `in ${days} days`;
}
