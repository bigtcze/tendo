import { useI18n } from '../../i18n';
import { PlusIcon } from './HomeIcons';

/**
 * The quick entry row that sits at the top of the list: it opens the same add form as the header
 * action, so the list itself invites the next entry without a second form implementation.
 */
export function AddItemRow({ onAdd }: { onAdd: () => void }) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      data-action="completion-trigger"
      onClick={onAdd}
      className="group/add -ml-2 flex w-full items-center gap-3 rounded-lg py-1.5 pr-2 text-left transition-colors hover:bg-sand/50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
    >
      <span aria-hidden="true"
        className="ml-0.5 grid size-7 shrink-0 place-items-center rounded-full border border-dashed border-field text-muted transition-colors group-hover/add:border-accent group-hover/add:text-accent">
        <PlusIcon className="size-3.5" />
      </span>
      <span className="min-w-0 text-body text-muted transition-colors group-hover/add:text-ink">{t('items.add.row')}</span>
    </button>
  );
}
