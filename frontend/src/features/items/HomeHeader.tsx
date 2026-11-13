import type { RefObject } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { PlusIcon } from './HomeIcons';

type HomeHeaderProps = {
  ready: boolean;
  adding: boolean;
  addRef: RefObject<HTMLButtonElement | null>;
  onAdd: () => void;
};

/**
 * One compact band: the screen name and its single primary action on the same line. The household name lives in the
 * header selector, so it is neither repeated on the page nor appended to the window title (every screen's title is
 * "<screen> — Tendo").
 */
export function HomeHeader({ ready, adding, addRef, onAdd }: HomeHeaderProps) {
  const { t } = useI18n();
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
      <Heading className="min-w-0 text-h1 font-semibold text-ink">
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
            className="min-h-11 gap-1.5 px-4"
          >
            <PlusIcon className="size-4" />
            {t('items.add')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
