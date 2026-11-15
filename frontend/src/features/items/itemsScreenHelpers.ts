import type { MessageKey } from '../../i18n';
import { pluralKey } from '../../lib/pluralKey';

/** A group heading's count ("3 items"), in the plural form the reader's language needs. */
export function countPluralKey(count: number, locale: string): MessageKey {
  return pluralKey('items.group.count', count, locale);
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
