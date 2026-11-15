import type { MessageKey } from '../i18n';

/**
 * A plain-language message and its values. A screen's live region or the Home notice speaks it; the only values are
 * household names and logins, never anything internal.
 */
export type NoticeMessage = { key: MessageKey; params?: Record<string, string> };
