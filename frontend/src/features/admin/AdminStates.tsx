import type { Translate } from '../../i18n';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Eyebrow } from '../../components/ui/eyebrow';
import { Notice } from '../../components/ui/notice';
import { Skeleton } from '../../components/ui/skeleton';
import type { NoticeSpec } from './useRoleChange';

/** Page opening shared by every list state, with the one live host that speaks action outcomes. */
export function AdminIntro({ notice, t }: { notice: NoticeSpec | null; t: Translate }) {
  return <>
    <Eyebrow>{t('admin.eyebrow')}</Eyebrow>
    <Heading className="mt-1.5 text-h1 leading-tight text-ink">{t('admin.title')}</Heading>
    <p className="mt-2 max-w-prose text-body text-muted">{t('admin.subtitle')}</p>
    <div role="status" aria-atomic="true" className="mt-4 empty:mt-0">
      {notice ? <Notice tone={notice.tone === 'success' ? 'success' : 'neutral'} role={null}>{t(notice.key, notice.params)}</Notice> : null}
    </div>
  </>;
}

/** Reset expiries use the household's locale and name both the day and the time. */
export function formatResetExpiry(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));
}

/** A calm raised surface stands in for the list while roles load, so the screen never jumps empty. */
export function AdminListSkeleton({ label }: { label: string }) {
  return <div role="status" className="mt-5 overflow-hidden rounded-xl border border-line bg-raised">
    <span className="sr-only">{label}</span>
    {[0, 1, 2].map((row) => (
      <div key={row} className="flex min-h-14 items-center gap-3 border-b border-line px-4 py-3 last:border-b-0">
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3.5 w-32 max-w-[45%]" />
          <Skeleton className="h-3 w-24 max-w-[35%]" />
        </div>
        <Skeleton className="h-11 w-28 max-w-[30%]" />
      </div>
    ))}
  </div>;
}

/** Members reach this by opening the address directly: the server refused, so no account data is shown. */
export function AdminOnlyAdmins({ t }: { t: Translate }) {
  return <section aria-labelledby="admin-restricted-heading" className="mt-5 rounded-xl border border-line bg-raised p-4 sm:p-5">
    <h2 id="admin-restricted-heading" className="text-h2 font-bold text-ink">{t('admin.onlyAdmins.title')}</h2>
    <p className="mt-1 max-w-prose text-small text-muted">{t('admin.onlyAdmins.body')}</p>
  </section>;
}

export function AdminLoadError({ notice, onRetry, t }: { notice: string; onRetry: () => void; t: Translate }) {
  return <div className="mt-4 space-y-3">
    <Notice tone="error" aria-atomic="true">{notice}</Notice>
    <Button onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}
