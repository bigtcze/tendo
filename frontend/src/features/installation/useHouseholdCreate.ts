import { useEffect, useMemo, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { codePointLength } from '../../lib/textLength';
import { detectTimezone, timezoneOptions } from '../auth/onboardingModel';
import {
  createAdminHousehold,
  type AdminHousehold, type AdminHouseholdCode, type AdminHouseholdCreate, type AdminHouseholdField,
  type DirectoryAccount,
} from './instanceAdminApi';
import { MAX_QUERY, useDirectorySearch } from './useDirectorySearch';

/** The server trims the name and accepts 1 to 100 code points; the client says so first and never outranks it. */
export const NAME_MAX = 100;

/** Why the dialog is still open after the server answered: a field it refused, or a failure with nothing to point at. */
export type CreateFailure =
  | { kind: 'field'; field: AdminHouseholdField; message: MessageKey }
  | { kind: 'error' };

type Outcomes = {
  viewerId: string;
  onSignedOut: () => void;
  /** An authoritative refusal of the installation grant: the shell re-reads the session. */
  onForbidden: () => void;
  /** The household exists; `asSelf` means the viewer is its first administrator, which the session read settles. */
  onCreated: (household: AdminHousehold, asSelf: boolean) => void;
};

/** Every refusal the server can name a field for, said in the reader's language. */
const refusalMessages: Record<AdminHouseholdCode, MessageKey> = {
  invalid_length: 'install.create.name.length',
  invalid_characters: 'install.create.name.characters',
  invalid_iana_timezone: 'install.create.timezone.invalid',
  invalid_reference: 'install.create.admin.invalid',
};

/** The trimmed name the client will send, or the one thing the reader has to fix before it can be sent. */
function localNameProblem(trimmed: string): MessageKey | null {
  const length = codePointLength(trimmed);
  if (length === 0) return 'install.create.name.empty';
  if (length > NAME_MAX) return 'install.create.name.long';
  return null;
}

/** Only the field this change is about stops saying what it said; anything else the server answered stays. */
function clearField(set: (update: (current: CreateFailure | null) => CreateFailure | null) => void, field: AdminHouseholdField) {
  set((current) => (current?.kind === 'field' && current.field === field ? null : current));
}

/** A failure with nothing to point at is answered by trying again; a refusal of a field stays until that field does. */
function clearFailure(set: (update: (current: CreateFailure | null) => CreateFailure | null) => void) {
  set((current) => (current?.kind === 'error' ? null : current));
}

/**
 * The one intent of the last attempt and the key it was sent with. An identical retry reuses the key; anything the
 * server fingerprints differently — the trimmed name, the timezone, the account — is a new intent and gets a new key.
 */
function intentKey(intent: { current: { signature: string; key: string } | null }, signature: string): string {
  const current = intent.current;
  if (current !== null && current.signature === signature) return current.key;
  const next = { signature, key: idempotencyKey() };
  intent.current = next;
  return next.key;
}

/** What the server's answer means to the dialog: a field to answer, a failure to keep it open on, or success. */
function settleCreate(
  result: AdminHouseholdCreate, target: DirectoryAccount, out: Outcomes, forgetIntent: () => void,
): { closed: boolean; failure: CreateFailure | null } {
  switch (result.kind) {
    case 'created':
      // The household exists: no later press is this intent again, so the next one starts with a key of its own.
      forgetIntent();
      out.onCreated(result.household, target.userId === out.viewerId);
      return { closed: true, failure: null };
    case 'invalid':
      return { closed: false, failure: { kind: 'field', field: result.field, message: refusalMessages[result.code] } };
    case 'unauthenticated':
      out.onSignedOut();
      return { closed: false, failure: null };
    case 'forbidden':
      out.onForbidden();
      return { closed: true, failure: null };
    case 'keyReused':
      // Unreachable by design, because one key never leaves its intent; if it happens, the next press starts over.
      forgetIntent();
      return { closed: false, failure: { kind: 'error' } };
    default:
      return { closed: false, failure: { kind: 'error' } };
  }
}

/**
 * The screen's one way of creating a household: a name, a timezone and an existing account that becomes its first
 * administrator. All three are required, so nothing is sent until they are all there.
 *
 * A create is not safely repeatable by value alone — two households may share a name — so one intent carries one
 * `Idempotency-Key`. The intent is the trimmed name, the timezone and the chosen account: retrying after a failure
 * reuses the key, so a response lost on the way back still returns the household already created, while changing any
 * of the three starts a genuinely new intent with a new key. The key survives closing the dialog, because reopening
 * it and pressing Create again can still be the same household the server may already hold.
 */
export function useHouseholdCreate(outcomes: Outcomes) {
  const [detected] = useState(detectTimezone);
  const zones = useMemo(() => timezoneOptions(detected), [detected]);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState('');
  const [timezone, setTimezone] = useState(detected);
  const [chosen, setChosen] = useState<DirectoryAccount | null>(null);
  const [query, setQuery] = useState('');
  const [failure, setFailure] = useState<CreateFailure | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const mountedRef = useRef(false);
  const outcomesRef = useRef(outcomes);
  const intentRef = useRef<{ signature: string; key: string } | null>(null);
  const directory = useDirectorySearch({
    onSignedOut: () => outcomesRef.current.onSignedOut(),
    // The grant is gone with the dialog: there is nothing left to create a household with here.
    onForbidden: () => { setOpen(false); outcomesRef.current.onForbidden(); },
    // Any new search or page replaces the choices on screen, so the account chosen against them is dropped with them.
    onReadStart: () => { setChosen(null); clearField(setFailure, 'adminUserId'); },
  });

  useEffect(() => { outcomesRef.current = outcomes; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  /** Opens the dialog on a clean form: no name, the browser's own zone, nothing chosen and nothing answered yet. */
  function start(trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    setOpen(true);
    setName('');
    setTimezone(detected);
    setChosen(null);
    setQuery('');
    setFailure(null);
    directory.reset();
  }

  function editName(value: string) { setName(value); clearField(setFailure, 'name'); }
  function editTimezone(value: string) { setTimezone(value); clearField(setFailure, 'timezone'); }
  function editQuery(value: string) { setQuery(value); clearField(setFailure, 'adminUserId'); }
  function choose(account: DirectoryAccount) { setChosen(account); clearField(setFailure, 'adminUserId'); }

  function searchDirectory() {
    if (busyRef.current) return;
    const needle = query.trim();
    // The server counts code points, so a search of 65 astral characters is refused here, not by the server.
    if (codePointLength(needle) > MAX_QUERY) { directory.refuse(needle); return; }
    // A search answers a failure with nothing to point at, but never a refusal the server aimed at a field.
    clearFailure(setFailure);
    directory.start(needle);
  }

  function cancel() {
    if (busyRef.current) return;
    setOpen(false);
  }

  async function submit() {
    if (busyRef.current || chosen === null) return;
    const trimmed = name.trim();
    const problem = localNameProblem(trimmed);
    if (problem) { setFailure({ kind: 'field', field: 'name', message: problem }); return; }
    const target = chosen;
    const out = outcomesRef.current;
    const signature = `${trimmed}\u0000${timezone}\u0000${target.userId}`;
    busyRef.current = true;
    setBusy(true);
    setFailure(null);
    const result = await createAdminHousehold(
      { name: trimmed, timezone, adminUserId: target.userId }, intentKey(intentRef, signature),
    );
    busyRef.current = false;
    if (!mountedRef.current) return;
    setBusy(false);
    const settled = settleCreate(result, target, out, () => { intentRef.current = null; });
    if (settled.closed) setOpen(false);
    setFailure(settled.failure);
  }

  return {
    open, busy, triggerRef, name, timezone, zones, chosen, failure, query,
    search: directory.search, queryError: directory.queryError, refusals: directory.refusals,
    canSubmit: chosen !== null && name.trim() !== '',
    start, editName, editTimezone, editQuery, searchDirectory, choose,
    next: directory.next, previous: directory.previous, retry: directory.retry, cancel, submit,
  };
}

export type HouseholdCreate = ReturnType<typeof useHouseholdCreate>;
