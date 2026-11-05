import { Button } from '../../components/ui/button';
import type { MessageKey } from '../../i18n';

export function SubjectsViewToggle({ showArchived, listKind, t, onSwitchView }: {
  showArchived: boolean; listKind: string; t: (key: MessageKey) => string; onSwitchView: () => void;
}) {
  return listKind === 'ready' || listKind === 'loading' ? <div className="mt-2 flex justify-start">
    <Button type="button" variant="quiet" onClick={onSwitchView} className="min-h-11 rounded-full bg-sand px-4">
      {showArchived ? t('subjects.showCurrent') : t('subjects.showArchived')}
    </Button>
  </div> : null;
}
