import type { Translate } from '../../i18n';
import { Heading } from '../../app/Heading';
import { ListSkeleton } from '../../components/ui/list-skeleton';
import { LoadError } from '../../components/ui/load-error';
import { Notice } from '../../components/ui/notice';
import type { NoticeSpec } from './useRoleChange';

/** Page opening shared by every list state, with the one live host that speaks action outcomes. */
export function AdminIntro({ notice, t }: { notice: NoticeSpec | null; t: Translate }) {
  return <>
    <Heading className="text-h1 font-semibold text-ink">{t('admin.title')}</Heading>
    <div role="status" aria-atomic="true" className="mt-6 empty:mt-0">
      {notice ? <Notice tone={notice.tone === 'success' ? 'success' : 'neutral'} role={null}>{t(notice.key, notice.params)}</Notice> : null}
    </div>
  </>;
}

/** Reset expiries use the household's locale and name both the day and the time. */
export function formatResetExpiry(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));
}

/** Flat placeholder rows in the list's own rhythm hold its place while roles load, so the screen never jumps empty. */
export function AdminListSkeleton({ label }: { label: string }) {
  return <ListSkeleton label={label} actions />;
}

/**
 * Members reach this by opening the address directly: the server refused, so no account data is shown. A plain
 * section like the member view of Members, not a card.
 */
export function AdminOnlyAdmins({ t }: { t: Translate }) {
  return <section aria-labelledby="admin-restricted-heading" className="mt-6 sm:mt-8">
    <h2 id="admin-restricted-heading" className="text-h2 text-ink">{t('admin.onlyAdmins.title')}</h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('admin.onlyAdmins.body')}</p>
  </section>;
}

export function AdminLoadError({ notice, onRetry, t }: { notice: string; onRetry: () => void; t: Translate }) {
  return <LoadError className="mt-8" message={notice} retryLabel={t('app.error.retry')} onRetry={onRetry} />;
}
