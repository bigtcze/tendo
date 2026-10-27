import { Button } from '../../components/ui/button';
import type { MessageKey } from '../../i18n';

export function SubjectsViewToggle({ showArchived, listKind, t, onSwitchView }: {
  showArchived: boolean; listKind: string; t: (key: MessageKey) => string; onSwitchView: () => void;
}) {
  return listKind === 'ready' || listKind === 'loading' ? <div className="mt-6 border-t border-line pt-4">
    <Button type="button" variant="quiet" size="small" onClick={onSwitchView}>
      {showArchived ? t('subjects.showCurrent') : t('subjects.showArchived')}
    </Button>
  </div> : null;
}
