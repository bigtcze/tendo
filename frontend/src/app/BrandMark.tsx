import { useI18n } from '../i18n';

/**
 * Brand tile and wordmark. In a household bar the wordmark is hidden below `sm`, where the bar must fit the brand, the
 * household selector and the account controls at 320 px; a screen-reader-only name keeps the brand announced there
 * too. A bar without those controls (`named`: signed out, setup, invitation, reset and consent pages) has room for the
 * name at every width, and those are the pages where a first-time visitor most needs to see which app this is.
 */
export function BrandMark({ named = false }: { named?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flex min-w-0 shrink-0 items-center gap-3">
      <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-xl bg-accent text-on-accent">
        <svg focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9"
          strokeLinecap="round" strokeLinejoin="round" className="size-5">
          <path d="M3.5 10.2 12 3.5l8.5 6.7" />
          <path d="M5.5 9v10.5h13V9" />
          <path d="M9.5 19v-5.5h5V19" />
        </svg>
      </span>
      {named ? (
        <span className="text-xl font-semibold tracking-tight text-ink">{t('app.name')}</span>
      ) : (
        <>
          <span aria-hidden="true" className="hidden text-xl font-semibold tracking-tight text-ink sm:inline">{t('app.name')}</span>
          <span className="sr-only sm:hidden">{t('app.name')}</span>
        </>
      )}
    </div>
  );
}
