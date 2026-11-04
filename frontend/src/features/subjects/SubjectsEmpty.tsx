import type { MessageKey } from '../../i18n';

export function SubjectsEmpty({ showArchived, cursorStackEmpty, adding, t }: {
  showArchived: boolean; cursorStackEmpty: boolean; adding: boolean; t: (key: MessageKey) => string;
}) {
  return cursorStackEmpty && !adding ? <div className="max-w-2xl rounded-2xl bg-raised p-6 sm:p-8">
    <h2 className="font-display text-2xl leading-snug">
      {showArchived ? t('subjects.emptyArchived') : t('subjects.empty.title')}
    </h2>
    {showArchived ? null : <p className="mt-3 text-muted">{t('subjects.empty.body')}</p>}
  </div> : null;
}
