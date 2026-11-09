import type { RefObject } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { PlusIcon } from './HomeIcons';

type HomeHeaderProps = {
  householdName: string;
  ready: boolean;
  adding: boolean;
  addRef: RefObject<HTMLButtonElement | null>;
  onAdd: () => void;
};

/** Eyebrow, household headline and the one primary action, sharing a single band on desktop. */
export function HomeHeader({ householdName, ready, adding, addRef, onAdd }: HomeHeaderProps) {
  const { t } = useI18n();
  return (
    <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between lg:gap-8">
      <div className="min-w-0">
        <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">
          <span className="hidden lg:inline">{t('home.eyebrow.desktop')}</span>
          <span className="lg:hidden">{t('home.eyebrow.mobile')}</span>
        </p>
        <Heading documentTitle={householdName} className="mt-2 text-[2.125rem] font-bold leading-[1.08] text-ink sm:text-display lg:text-[2.5rem]">
          {householdName}<span aria-hidden="true" className="text-accent">.</span>
        </Heading>
        <p className="mt-2 max-w-2xl text-base text-muted">{t('home.tagline')}</p>
      </div>
      {ready ? (
        <div className={`shrink-0 lg:pb-1 ${adding ? 'invisible' : ''}`}>
          <Button
            ref={addRef}
            data-action="completion-trigger"
            type="button"
            aria-hidden={adding || undefined}
            tabIndex={adding ? -1 : undefined}
            onClick={onAdd}
            className="w-full lg:w-auto"
          >
            <PlusIcon />
            {t('items.add')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
