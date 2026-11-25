import { useCallback } from 'react';
import {
  readAiAppsSettings, saveAiAppsSettings, type AiAppsAccess, type AiAppsDraft, type AiAppsSettings,
} from './aiAppsApi';
import { useVersionedForm } from './useVersionedForm';

export const ACCESS_ORDER: AiAppsAccess[] = ['tendo:read', 'tendo:write'];

/** Read before change, whichever was chosen first: the order the server answers in. */
export function orderedAccess(chosen: Iterable<AiAppsAccess>): AiAppsAccess[] {
  const set = new Set(chosen);
  return ACCESS_ORDER.filter((scope) => set.has(scope));
}

/** One kind of access turned on or off in a list of them. */
export function toggledAccess(current: AiAppsAccess[], scope: AiAppsAccess, on: boolean): AiAppsAccess[] {
  const chosen = new Set(current);
  if (on) chosen.add(scope); else chosen.delete(scope);
  return orderedAccess(chosen);
}

function draftOf(settings: AiAppsSettings): AiAppsDraft {
  return {
    enabled: settings.enabled,
    defaultAccountEnabled: settings.defaultAccountEnabled,
    defaultMaxScopes: orderedAccess(settings.defaultMaxScopes),
  };
}

/**
 * The installation's AI app settings: one read, an editable copy, and a save that names the version it was read at.
 * Nothing changes on screen before the server agrees; a save someone else overtook reads the settings again.
 */
export function useAiAppsSettings({ onSignedOut, onForbidden }: {
  onSignedOut: () => void;
  /** `duringSave` says the refusal took a form the reader was using, so focus has somewhere to go. */
  onForbidden: (duringSave: boolean) => void;
}) {
  const form = useVersionedForm<AiAppsSettings, AiAppsDraft>({
    read: readAiAppsSettings,
    write: saveAiAppsSettings,
    draftOf,
    // The server needs at least one kind of access; say so on the field instead of sending a save that must fail.
    refusedLocally: (draft) => draft.defaultMaxScopes.length === 0,
    messages: {
      saved: 'aiApps.notice.saved', stale: 'aiApps.notice.stale',
      rejected: 'aiApps.error.rejected', failed: 'aiApps.error.save',
    },
    onSignedOut,
    onForbidden,
  });
  const { edit } = form;

  const update = useCallback((change: Pick<Partial<AiAppsDraft>, 'enabled' | 'defaultAccountEnabled'>) => {
    edit((current) => ({ ...current, ...change }));
  }, [edit]);

  const toggleAccess = useCallback((scope: AiAppsAccess, on: boolean) => {
    edit((current) => ({ ...current, defaultMaxScopes: toggledAccess(current.defaultMaxScopes, scope, on) }));
  }, [edit]);

  return {
    state: form.state, settings: form.value, draft: form.draft, saving: form.saving, outcome: form.outcome,
    accessMissing: form.fieldInvalid, retry: form.retry, update, toggleAccess, save: form.save,
  };
}

export type AiAppsSettingsState = ReturnType<typeof useAiAppsSettings>;
