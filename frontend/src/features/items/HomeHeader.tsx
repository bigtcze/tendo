import type { RefObject } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Eyebrow } from '../../components/ui/eyebrow';
import { useI18n } from '../../i18n';
import { PlusIcon } from './HomeIcons';

type HomeHeaderProps = {
  householdName: string;
  ready: boolean;
  adding: boolean;
  addRef: RefObject<HTMLButtonElement | null>;
  onAdd: () => void;
};

/**
 * Eyebrow, household headline and the one primary action in a single band. From `lg` the action sits in
 * the headline row and shares its baseline block, as in the desktop reference; on phones it keeps the
 * last row below the supporting sentence, full width.
 */
export function HomeHeader({ householdName, ready, adding, addRef, onAdd }: HomeHeaderProps) {
  const { t } = useI18n();
  return (
    <div className="grid gap-y-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:gap-x-8 lg:gap-y-2">
      <div className="col-start-1 row-start-1 min-w-0">
        <Eyebrow>
          <span className="hidden lg:inline">{t('home.eyebrow.desktop')}</span>
          <span className="lg:hidden">{t('home.eyebrow.mobile')}</span>
        </Eyebrow>
        <Heading documentTitle={householdName} className="mt-2 text-[2.125rem] font-bold leading-[1.08] text-ink sm:text-display lg:text-[2.5rem]">
          {householdName}<span aria-hidden="true" className="text-accent">.</span>
        </Heading>
      </div>
      <p className="col-start-1 row-start-2 max-w-2xl text-base text-muted">{t('home.tagline')}</p>
      {ready ? (
        <div className={`col-start-1 row-start-3 shrink-0 lg:col-start-2 lg:row-start-1 ${adding ? 'invisible' : ''}`}>
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
