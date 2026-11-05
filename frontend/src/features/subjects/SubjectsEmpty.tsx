import { EmptyState } from '../../components/ui/empty-state';
import type { MessageKey } from '../../i18n';

export function SubjectsEmpty({ showArchived, cursorStackEmpty, adding, t }: {
  showArchived: boolean; cursorStackEmpty: boolean; adding: boolean; t: (key: MessageKey) => string;
}) {
  return cursorStackEmpty && !adding ? <div className="max-w-2xl">
    <EmptyState title={showArchived ? t('subjects.emptyArchived') : t('subjects.empty.title')}>
      {showArchived ? null : t('subjects.empty.body')}
    </EmptyState>
  </div> : null;
}
