import { useCallback } from 'react';
import {
  readOwnAiApps, saveOwnAiApps, type AiAppsAccess, type OwnAiApps,
} from '../installation/aiAppsApi';
import { orderedAccess, toggledAccess } from '../installation/useAiAppsSettings';
import { useVersionedForm } from '../installation/useVersionedForm';

export type OwnAiAppsDraft = { enabled: boolean; scopes: AiAppsAccess[] };

function draftOf(settings: OwnAiApps): OwnAiAppsDraft {
  return { enabled: settings.enabled, scopes: orderedAccess(settings.scopes) };
}

/** The settings every account starts with: on, read and change. Anything else is the person's own narrowing. */
export function isOwnDefault(settings: OwnAiApps): boolean {
  return settings.enabled && settings.scopes.includes('tendo:read') && settings.scopes.includes('tendo:write');
}

/**
 * Whether Account shows the section: AI apps are allowed for the account, or the person narrowed their own choice,
 * which stays theirs to undo. Installations that never allow AI apps show nothing.
 */
export function isOwnAiAppsRelevant(settings: OwnAiApps): boolean {
  return settings.maximumScopes.length > 0 || !isOwnDefault(settings);
}

/**
 * The signed-in person's own AI apps: their switch and what their apps may do, saved with the version they were read
 * at, beside what they are allowed and what apps may do now, which only the server's answer ever changes. The own
 * choice can only narrow: choosing something the administrator does not allow is stored and grants nothing.
 */
export function useOwnAiApps({ onSignedOut }: { onSignedOut: () => void }) {
  const form = useVersionedForm<OwnAiApps, OwnAiAppsDraft>({
    read: readOwnAiApps,
    write: (draft, version) => saveOwnAiApps(draft, version),
    draftOf,
    // The server needs at least one kind of access; say so on the field instead of sending a save that must fail.
    refusedLocally: (draft) => draft.scopes.length === 0,
    messages: {
      saved: 'ownAiApps.notice.saved', stale: 'ownAiApps.notice.stale',
      rejected: 'aiApps.error.rejected', failed: 'aiApps.error.save', rateLimited: 'ownAiApps.error.rateLimited',
    },
    onSignedOut,
    // Nothing on this resource needs the installation grant; a 403 is a refusal like any other.
    onForbidden: () => {},
  });
  const { edit } = form;
  const setEnabled = useCallback((enabled: boolean) => edit((current) => ({ ...current, enabled })), [edit]);
  const toggleAccess = useCallback((scope: AiAppsAccess, on: boolean) => {
    edit((current) => ({ ...current, scopes: toggledAccess(current.scopes, scope, on) }));
  }, [edit]);

  return {
    state: form.state, settings: form.value, draft: form.draft, saving: form.saving, outcome: form.outcome,
    accessMissing: form.fieldInvalid, retry: form.retry, setEnabled, toggleAccess, save: form.save,
  };
}

export type OwnAiAppsState = ReturnType<typeof useOwnAiApps>;
