import { useCallback } from 'react';
import {
  readAccountAiApps, saveAccountAiApps, type AccountAiApps, type AccountAiAppsUpdate, type AiAppsAccess,
} from './aiAppsApi';
import { orderedAccess } from './useAiAppsSettings';
import { useVersionedForm } from './useVersionedForm';

/** The administrator's switch for one account: the installation's default for accounts, or a choice of its own. */
export type AccountSwitch = 'default' | 'on' | 'off';
/** The most apps may do for one account: the installation's default maximum, or one of the three it can be. */
export type AccountMaximum = 'default' | 'read' | 'write' | 'both';
export type AccountAiAppsDraft = { enabled: AccountSwitch; maximum: AccountMaximum };
/** A set of access in the words the screen uses for it; `none` is the empty set. */
export type AccessWord = 'none' | 'read' | 'write' | 'both';

export function accessWord(scopes: readonly AiAppsAccess[]): AccessWord {
  const read = scopes.includes('tendo:read');
  const write = scopes.includes('tendo:write');
  if (read && write) return 'both';
  if (read) return 'read';
  return write ? 'write' : 'none';
}

const SCOPES: Record<Exclude<AccountMaximum, 'default'>, NonNullable<AccountAiAppsUpdate['maxScopes']>> = {
  read: ['tendo:read'],
  write: ['tendo:write'],
  both: ['tendo:read', 'tendo:write'],
};

function draftOf(policy: AccountAiApps): AccountAiAppsDraft {
  const enabled: AccountSwitch = policy.enabled === null ? 'default' : (policy.enabled ? 'on' : 'off');
  // A stored maximum is never empty, so it is always one of the three choices.
  const word = policy.maxScopes === null ? 'default' : accessWord(orderedAccess(policy.maxScopes));
  return { enabled, maximum: word === 'none' ? 'default' : word };
}

/** The full replacement the server stores: null hands that term back to the installation default. */
function updateOf(draft: AccountAiAppsDraft): AccountAiAppsUpdate {
  return {
    enabled: draft.enabled === 'default' ? null : draft.enabled === 'on',
    maxScopes: draft.maximum === 'default' ? null : SCOPES[draft.maximum],
  };
}

/**
 * One account's AI apps: the administrator's choice for it, which the form edits and saves with the version it was
 * read at, beside the owner's own choice and what apps may do now, which only the server's answer ever changes.
 */
export function useAccountAiApps({ userId, onSignedOut, onForbidden }: {
  userId: string;
  onSignedOut: () => void;
  onForbidden: (duringSave: boolean) => void;
}) {
  const form = useVersionedForm<AccountAiApps, AccountAiAppsDraft>({
    read: () => readAccountAiApps(userId),
    write: (draft, version) => saveAccountAiApps(userId, updateOf(draft), version),
    draftOf,
    // Every choice on this form is one the server accepts; a refusal of the maximum is still said on its field.
    refusedLocally: () => false,
    messages: {
      saved: 'accountAiApps.notice.saved', stale: 'accountAiApps.notice.stale',
      rejected: 'aiApps.error.rejected', failed: 'aiApps.error.save',
    },
    onSignedOut,
    onForbidden,
  });
  const { edit } = form;
  const setSwitch = useCallback((enabled: AccountSwitch) => edit((current) => ({ ...current, enabled })), [edit]);
  const setMaximum = useCallback((maximum: AccountMaximum) => edit((current) => ({ ...current, maximum })), [edit]);

  return {
    state: form.state, policy: form.value, draft: form.draft, saving: form.saving, outcome: form.outcome,
    maximumRefused: form.fieldInvalid, retry: form.retry, setSwitch, setMaximum, save: form.save,
  };
}

export type AccountAiAppsState = ReturnType<typeof useAccountAiApps>;
