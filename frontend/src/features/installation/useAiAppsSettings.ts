import { useCallback, useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import {
  readAiAppsSettings, saveAiAppsSettings, type AiAppsAccess, type AiAppsDraft, type AiAppsRead, type AiAppsSave,
  type AiAppsSettings,
} from './aiAppsApi';

/** `error` is worth a retry; `refused` is an answer a retry would only repeat. */
export type AiAppsState = 'loading' | 'ready' | 'forbidden' | 'error' | 'refused';
/** What the last save said; `error` outcomes go to the alert host, the others to the polite one. */
export type AiAppsOutcome = { key: MessageKey; tone: 'success' | 'neutral' | 'error' } | null;

const SAVED: AiAppsOutcome = { key: 'aiApps.notice.saved', tone: 'success' };
const STALE: AiAppsOutcome = { key: 'aiApps.notice.stale', tone: 'neutral' };
type SaveResult = 'saved' | 'invalid' | 'other';

const ACCESS_ORDER: AiAppsAccess[] = ['tendo:read', 'tendo:write'];

function draftOf(settings: AiAppsSettings): AiAppsDraft {
  return {
    enabled: settings.enabled,
    defaultAccountEnabled: settings.defaultAccountEnabled,
    defaultMaxScopes: ACCESS_ORDER.filter((scope) => settings.defaultMaxScopes.includes(scope)),
  };
}

/** The editable copy of the settings. Every edit calls `onEdit`: what the last save said no longer describes it. */
function useAiAppsDraft(onEdit: () => void) {
  const [draft, setDraft] = useState<AiAppsDraft | null>(null);
  const [accessMissing, setAccessMissing] = useState(false);

  const update = useCallback((change: Pick<Partial<AiAppsDraft>, 'enabled' | 'defaultAccountEnabled'>) => {
    onEdit();
    setDraft((current) => (current ? { ...current, ...change } : current));
  }, [onEdit]);

  const toggleAccess = useCallback((scope: AiAppsAccess, on: boolean) => {
    if (!draft) return;
    const chosen = new Set(draft.defaultMaxScopes);
    if (on) chosen.add(scope); else chosen.delete(scope);
    if (chosen.size > 0) setAccessMissing(false);
    onEdit();
    setDraft({ ...draft, defaultMaxScopes: ACCESS_ORDER.filter((entry) => chosen.has(entry)) });
  }, [draft, onEdit]);

  return { draft, setDraft, accessMissing, setAccessMissing, update, toggleAccess };
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
  const [state, setState] = useState<AiAppsState>('loading');
  const [settings, setSettings] = useState<AiAppsSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [outcome, setOutcome] = useState<AiAppsOutcome>(null);
  const clearOutcome = useCallback(() => setOutcome(null), []);
  const { draft, setDraft, accessMissing, setAccessMissing, update, toggleAccess } = useAiAppsDraft(clearOutcome);
  const versionRef = useRef<string | null>(null);
  // The guard lives in a ref: two submits before the next render must still send one save.
  const savingRef = useRef(false);
  const generationRef = useRef(0);
  const handlersRef = useRef({ onSignedOut, onForbidden });
  useEffect(() => { handlersRef.current = { onSignedOut, onForbidden }; }, [onSignedOut, onForbidden]);

  const adopt = useCallback((next: AiAppsSettings, version: string) => {
    versionRef.current = version;
    setSettings(next);
    setDraft(draftOf(next));
    setAccessMissing(false);
    setState('ready');
  }, [setAccessMissing, setDraft]);

  const forbid = useCallback((duringSave: boolean) => {
    versionRef.current = null;
    setSettings(null);
    setDraft(null);
    setState('forbidden');
    handlersRef.current.onForbidden(duringSave);
  }, [setDraft]);

  /** Settles one read; only the newest read may change the screen. */
  const settle = useCallback((token: number, result: AiAppsRead, duringSave: boolean) => {
    if (token !== generationRef.current) return false;
    if (result.kind === 'ok') { adopt(result.settings, result.version); return true; }
    if (result.kind === 'unauthenticated') handlersRef.current.onSignedOut();
    else if (result.kind === 'forbidden') forbid(duringSave);
    else setState(result.kind === 'rejected' ? 'refused' : 'error');
    return false;
  }, [adopt, forbid]);

  /** Reads again; `duringSave` keeps the current form on screen while the read is on its way. */
  const load = useCallback(async (duringSave: boolean) => {
    const token = ++generationRef.current;
    if (!duringSave) setState('loading');
    return settle(token, await readAiAppsSettings(), duringSave);
  }, [settle]);

  // The first read: the state already starts as loading, so nothing is set before the answer arrives.
  useEffect(() => {
    const token = ++generationRef.current;
    void readAiAppsSettings().then((result) => settle(token, result, false));
    return () => { generationRef.current += 1; };
  }, [settle]);

  const retry = useCallback(() => { setOutcome(null); void load(false); }, [load]);

  /** Settles one save. A save someone else overtook keeps the form disabled until the settings are read again. */
  const settleSave = useCallback(async (result: AiAppsSave): Promise<SaveResult> => {
    if (result.kind === 'ok') { adopt(result.settings, result.version); setOutcome(SAVED); return 'saved'; }
    if (result.kind === 'unauthenticated') handlersRef.current.onSignedOut();
    else if (result.kind === 'forbidden') forbid(true);
    else if (result.kind === 'invalidAccess') { setAccessMissing(true); return 'invalid'; }
    // Both read again; a failed re-read leaves the screen's own load error, which already offers the retry.
    else if (result.kind === 'stale' || result.kind === 'savedUnversioned') {
      const read = await load(true);
      if (read) setOutcome(result.kind === 'stale' ? STALE : SAVED);
    }
    else setOutcome({ key: result.kind === 'rejected' ? 'aiApps.error.rejected' : 'aiApps.error.save', tone: 'error' });
    return 'other';
  }, [adopt, forbid, load, setAccessMissing]);

  const save = useCallback(async (): Promise<SaveResult> => {
    if (!draft || !versionRef.current || savingRef.current) return 'other';
    // The server needs at least one kind of access; say so on the field instead of sending a save that must fail.
    if (draft.defaultMaxScopes.length === 0) { setAccessMissing(true); setOutcome(null); return 'invalid'; }
    savingRef.current = true;
    setSaving(true);
    setOutcome(null);
    const token = generationRef.current;
    try {
      const result = await saveAiAppsSettings(draft, versionRef.current);
      return token === generationRef.current ? await settleSave(result) : 'other';
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }, [draft, setAccessMissing, settleSave]);

  return { state, settings, draft, saving, outcome, accessMissing, retry, update, toggleAccess, save };
}

export type AiAppsSettingsState = ReturnType<typeof useAiAppsSettings>;
