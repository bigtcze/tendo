import type { Translate } from '../../i18n';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { Skeleton } from '../../components/ui/skeleton';

/** A calm white surface stands in for the list while roles load, so the screen never jumps empty. */
export function AdminListSkeleton({ label }: { label: string }) {
  return <div role="status" className="mt-6 overflow-hidden rounded-2xl border border-line bg-raised sm:mt-8">
    <span className="sr-only">{label}</span>
    {[0, 1, 2].map((row) => (
      <div key={row} className="flex min-h-16 items-center gap-3 border-b border-line px-4 py-3 last:border-b-0">
        <Skeleton className="size-9 shrink-0 rounded-full" />
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
  return <section aria-labelledby="admin-restricted-heading" className="mt-6 rounded-2xl border border-line bg-raised p-5 sm:mt-8 sm:p-6">
    <h2 id="admin-restricted-heading" className="text-lg font-bold text-ink">{t('admin.onlyAdmins.title')}</h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('admin.onlyAdmins.body')}</p>
  </section>;
}

export function AdminLoadError({ notice, onRetry, t }: { notice: string; onRetry: () => void; t: Translate }) {
  return <div className="mt-8 space-y-3">
    <Notice tone="error" aria-atomic="true">{notice}</Notice>
    <Button onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}
