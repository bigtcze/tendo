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

/**
 * One line: a compact "Overview" page title and the single primary action. The household name is the switcher's
 * job in the top bar, so it lives in the document title only. Same row on every width.
 */
export function HomeHeader({ householdName, ready, adding, addRef, onAdd }: HomeHeaderProps) {
  const { t } = useI18n();
  return (
    <div className="flex min-w-0 items-center justify-between gap-4">
      <Heading documentTitle={`${t('home.overview')} · ${householdName}`} className="min-w-0 text-h1 font-bold leading-tight text-ink">
        {t('home.overview')}
      </Heading>
      {ready ? (
        <div className={`shrink-0 ${adding ? 'invisible' : ''}`}>
          <Button
            ref={addRef}
            data-action="completion-trigger"
            type="button"
            aria-hidden={adding || undefined}
            tabIndex={adding ? -1 : undefined}
            onClick={onAdd}
            className="min-h-11 px-4"
          >
            <PlusIcon className="size-4" />
            {t('items.add')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
