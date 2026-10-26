import type { MessageKey } from '../../i18n';

export function countPluralKey(count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = {
    en: ['one', 'other'],
    cs: ['one', 'few', 'other'],
  };
  return `items.group.count.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

// True while focus is still where an asynchronous action left it: on body (its control unmounted), on the
// control that started it, or inside the notice area. Late focus moves must not steal focus the user moved.
export function focusUnclaimed(origin: Element | null): boolean {
  const active = document.activeElement;
  return (
    !active ||
    active === document.body ||
    active === origin ||
    Boolean(active.closest('[data-items-notice]'))
  );
}
