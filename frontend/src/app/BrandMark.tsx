import { useI18n } from '../i18n';

/** Teal house tile and wordmark. Direction B drops the decorative tagline so the top bar carries only the brand. */
export function BrandMark() {
  const { t } = useI18n();
  return (
    <div className="flex min-w-0 shrink-0 items-center gap-2.5">
      <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-on-accent">
        <svg focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9"
          strokeLinecap="round" strokeLinejoin="round" className="size-4.5">
          <path d="M3.5 10.2 12 3.5l8.5 6.7" />
          <path d="M5.5 9v10.5h13V9" />
          <path d="M9.5 19v-5.5h5V19" />
        </svg>
      </span>
      <span className="text-h2 font-semibold tracking-tight text-ink">{t('app.name')}</span>
    </div>
  );
}
