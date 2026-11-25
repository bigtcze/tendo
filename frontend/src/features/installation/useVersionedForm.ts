import { useCallback, useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import type { VersionedRead, VersionedSave } from './aiAppsApi';

/** `error` is worth a retry; `refused` is an answer a retry would only repeat; `notFound` is a target that is gone. */
export type VersionedFormState = 'loading' | 'ready' | 'forbidden' | 'notFound' | 'error' | 'refused';
/** What the last save said; `error` outcomes go to the alert host, the others to the polite one. */
export type VersionedOutcome = { key: MessageKey; tone: 'success' | 'neutral' | 'error' } | null;
export type SaveResult = 'saved' | 'invalid' | 'other';

export type VersionedFormOptions<T, D> = {
  read: () => Promise<VersionedRead<T>>;
  write: (draft: D, version: string) => Promise<VersionedSave<T>>;
  draftOf: (value: T) => D;
  /** A draft the server must refuse: it is answered on the field instead of by a request that must fail. */
  refusedLocally: (draft: D) => boolean;
  messages: { saved: MessageKey; stale: MessageKey; rejected: MessageKey; failed: MessageKey };
  onSignedOut: () => void;
  /** `duringSave` says the refusal took a form the reader was using, so focus has somewhere to go. */
  onForbidden: (duringSave: boolean) => void;
};

/**
 * One server-held settings form: one read, an editable copy, and a save that names the version it was read at.
 * Nothing changes on screen before the server agrees; a save someone else overtook reads the settings again, and
 * only the newest read may change the screen. The options are read when they are used, so callers need not keep
 * them stable; a screen for another target mounts a new form.
 */
export function useVersionedForm<T, D>(options: VersionedFormOptions<T, D>) {
  const [state, setState] = useState<VersionedFormState>('loading');
  const [value, setValue] = useState<T | null>(null);
  const [draft, setDraft] = useState<D | null>(null);
  const draftRef = useRef<D | null>(null);
  const [fieldInvalid, setFieldInvalid] = useState(false);
  const [saving, setSaving] = useState(false);
  const [outcome, setOutcome] = useState<VersionedOutcome>(null);
  const versionRef = useRef<string | null>(null);
  // The guard lives in a ref: two submits before the next render must still send one save.
  const savingRef = useRef(false);
  const generationRef = useRef(0);
  const optionsRef = useRef(options);
  useEffect(() => { optionsRef.current = options; });

  const adopt = useCallback((next: T, version: string) => {
    versionRef.current = version;
    draftRef.current = optionsRef.current.draftOf(next);
    setValue(next); setDraft(draftRef.current); setFieldInvalid(false); setState('ready');
  }, []);

  const drop = useCallback((next: VersionedFormState) => {
    versionRef.current = null; draftRef.current = null;
    setValue(null); setDraft(null); setState(next);
  }, []);

  /** Settles one read; only the newest read may change the screen. */
  const settle = useCallback((token: number, result: VersionedRead<T>, duringSave: boolean) => {
    if (token !== generationRef.current) return false;
    if (result.kind === 'ok') { adopt(result.value, result.version); return true; }
    if (result.kind === 'unauthenticated') optionsRef.current.onSignedOut();
    else if (result.kind === 'forbidden') { drop('forbidden'); optionsRef.current.onForbidden(duringSave); }
    else if (result.kind === 'notFound') drop('notFound');
    else setState(result.kind === 'rejected' ? 'refused' : 'error');
    return false;
  }, [adopt, drop]);

  /** Reads again; `duringSave` keeps the current form on screen while the read is on its way. */
  const load = useCallback(async (duringSave: boolean) => {
    const token = ++generationRef.current;
    if (!duringSave) setState('loading');
    return settle(token, await optionsRef.current.read(), duringSave);
  }, [settle]);

  // The first read: the state already starts as loading, so nothing is set before the answer arrives.
  useEffect(() => {
    const token = ++generationRef.current;
    void optionsRef.current.read().then((result) => settle(token, result, false));
    return () => { generationRef.current += 1; };
  }, [settle]);

  const retry = useCallback(() => { setOutcome(null); void load(false); }, [load]);
  /** Every edit drops what the last save said, which no longer describes the form. */
  const edit = useCallback((change: (current: D) => D) => {
    // The ref, not the rendered draft: two edits before the next render must both land.
    if (draftRef.current === null) return;
    const next = change(draftRef.current);
    draftRef.current = next;
    setOutcome(null);
    if (!optionsRef.current.refusedLocally(next)) setFieldInvalid(false);
    setDraft(next);
  }, []);

  /** Settles one save. A save someone else overtook keeps the form disabled until the settings are read again. */
  const settleSave = useCallback(async (result: VersionedSave<T>): Promise<SaveResult> => {
    const { messages } = optionsRef.current;
    if (result.kind === 'ok') { adopt(result.value, result.version); setOutcome({ key: messages.saved, tone: 'success' }); return 'saved'; }
    if (result.kind === 'unauthenticated') optionsRef.current.onSignedOut();
    else if (result.kind === 'forbidden') { drop('forbidden'); optionsRef.current.onForbidden(true); }
    else if (result.kind === 'notFound') drop('notFound');
    else if (result.kind === 'invalidAccess') { setFieldInvalid(true); return 'invalid'; }
    // Both read again; a failed re-read leaves the screen's own load error, which already offers the retry.
    else if (result.kind === 'stale' || result.kind === 'savedUnversioned') {
      const read = await load(true);
      if (read) setOutcome(result.kind === 'stale' ? { key: messages.stale, tone: 'neutral' } : { key: messages.saved, tone: 'success' });
    }
    else setOutcome({ key: result.kind === 'rejected' ? messages.rejected : messages.failed, tone: 'error' });
    return 'other';
  }, [adopt, drop, load]);

  const save = useCallback(async (): Promise<SaveResult> => {
    if (!draft || !versionRef.current || savingRef.current) return 'other';
    if (optionsRef.current.refusedLocally(draft)) { setFieldInvalid(true); setOutcome(null); return 'invalid'; }
    savingRef.current = true;
    setSaving(true); setOutcome(null);
    const token = generationRef.current;
    try {
      const result = await optionsRef.current.write(draft, versionRef.current);
      return token === generationRef.current ? await settleSave(result) : 'other';
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }, [draft, settleSave]);

  return { state, value, draft, saving, outcome, fieldInvalid, retry, edit, save };
}

export type VersionedForm<T, D> = ReturnType<typeof useVersionedForm<T, D>>;
