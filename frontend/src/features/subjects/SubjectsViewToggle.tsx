import { Button } from '../../components/ui/button';
import type { MessageKey } from '../../i18n';

/**
 * Switching between the current and the archived list is a quiet, ordinary secondary control: the list is the
 * subject of the screen, so this stays a plain 44 px text button rather than a filled pill.
 */
export function SubjectsViewToggle({ showArchived, listKind, t, onSwitchView }: {
  showArchived: boolean; listKind: string; t: (key: MessageKey) => string; onSwitchView: () => void;
}) {
  return listKind === 'ready' || listKind === 'loading' ? <div className="mt-2 flex justify-start">
    <Button type="button" variant="quiet" size="small" onClick={onSwitchView} className="min-h-11">
      {showArchived ? t('subjects.showCurrent') : t('subjects.showArchived')}
    </Button>
  </div> : null;
}
