import { Button } from '../../components/ui/button';
import type { MessageKey } from '../../i18n';

export function SubjectsEmpty({ showArchived, cursorStackEmpty, adding, t, onAdd }: {
  showArchived: boolean; cursorStackEmpty: boolean; adding: boolean; t: (key: MessageKey) => string; onAdd: () => void;
}) {
  return cursorStackEmpty && !adding ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10">
    <h2 className="font-display text-2xl leading-snug">
      {showArchived ? t('subjects.emptyArchived') : t('subjects.empty.title')}
    </h2>
    {showArchived ? null : <>
      <p className="mt-3 text-muted">{t('subjects.empty.body')}</p>
      <Button type="button" data-focus="add" onClick={onAdd} className="mt-6 w-full sm:w-auto">{t('subjects.add')}</Button>
    </>}
  </div> : null;
}
