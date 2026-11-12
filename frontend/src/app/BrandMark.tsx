import { useI18n } from '../i18n';

/**
 * Tendo's own mark: the house outline in a pine tile beside the wordmark. Nothing promotional —
 * the household name lives in the header selector, where it is useful, not decorative.
 */
export function BrandMark() {
  const { t } = useI18n();
  return (
    <span className="flex min-w-0 shrink-0 items-center gap-2.5">
      <span aria-hidden="true" className="grid size-7 shrink-0 place-items-center rounded-lg bg-accent text-on-accent">
        <svg focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
          strokeLinecap="round" strokeLinejoin="round" className="size-[1.125rem]">
          <path d="M3.5 10.2 12 3.5l8.5 6.7" />
          <path d="M5.5 9v10.5h13V9" />
          <path d="M9.5 19v-5.5h5V19" />
        </svg>
      </span>
      <span className="text-[1.0625rem] font-semibold tracking-[-0.015em] text-ink max-[359px]:hidden">{t('app.name')}</span>
    </span>
  );
}
