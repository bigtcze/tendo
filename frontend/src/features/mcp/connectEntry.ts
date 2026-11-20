/**
 * The consent entry the authorization server leaves in the address fragment, and the short-lived record that
 * carries the handle across a sign-in. The handle is a bearer secret for one pending consent: it lives in the
 * fragment (never sent to a server) and in sessionStorage only while the person is finishing the consent.
 */

export const CONNECT_ERROR_CODES = [
  'invalid_request',
  'unsupported_response_type',
  'invalid_scope',
  'invalid_target',
  'invalid_client',
  'invalid_redirect_uri',
  'temporarily_unavailable',
  'server_error',
] as const;
export type ConnectErrorCode = (typeof CONNECT_ERROR_CODES)[number];

export type ConnectFragment =
  | { kind: 'request'; handle: string }
  | { kind: 'error'; code: ConnectErrorCode }
  | { kind: 'invalid' };

const HANDLE = /^[A-Za-z0-9_-]{43}$/;

export function isConnectHandle(value: unknown): value is string {
  return typeof value === 'string' && HANDLE.test(value);
}

/**
 * Exactly one known key is accepted. Duplicates, extra keys, mixed keys and bad values are invalid, and raw
 * fragment text never leaves this function except as a validated handle or code.
 */
export function parseConnectFragment(hash: string): ConnectFragment | null {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash;
  if (raw === '') return null;
  const params = new URLSearchParams(raw);
  const keys = [...params.keys()];
  const key = keys[0];
  if (keys.length !== 1 || key === undefined) return { kind: 'invalid' };
  const value = params.get(key) ?? '';
  if (key === 'request' && isConnectHandle(value)) return { kind: 'request', handle: value };
  if (key === 'error' && (CONNECT_ERROR_CODES as readonly string[]).includes(value)) {
    return { kind: 'error', code: value as ConnectErrorCode };
  }
  return { kind: 'invalid' };
}

// Test seam: the clock is injectable so the freshness rules are testable without real waiting.
let clock: () => number = () => Date.now();
export function setConnectClockForTests(next: (() => number) | null): void {
  clock = next ?? (() => Date.now());
}

const STORAGE_KEY = 'tendo.mcpConnect';
const MAX_AGE_MS = 5 * 60 * 1000;
const SKEW_MS = 5 * 1000;
const STORED_KEYS = 'handle,returnAfterSignIn,savedAt';

export type ConnectEntry = { handle: string; savedAt: number; returnAfterSignIn: boolean };

/*
 * Storage rules, mirroring pendingItemCreate: every read revalidates, and once storage fails (missing, throwing, or a
 * failed write/removal) memory is authoritative until a storage write or removal succeeds again. Memory then answers
 * instead of a possibly stale stored copy.
 */
let memoryEntry: ConnectEntry | null = null;
let memoryAuthoritative = false;

function storage(): Storage | undefined {
  try { return globalThis.sessionStorage; } catch { return undefined; }
}

function isFresh(entry: ConnectEntry): boolean {
  const age = clock() - entry.savedAt;
  return age >= -SKEW_MS && age <= MAX_AGE_MS;
}

/** Revalidates every field: a tampered, extra-keyed, future-dated or expired record is not an entry at all. */
function validateStored(value: unknown): ConnectEntry | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  if (Object.keys(record).sort().join(',') !== STORED_KEYS) return null;
  if (!isConnectHandle(record.handle)) return null;
  if (typeof record.savedAt !== 'number' || !Number.isFinite(record.savedAt)) return null;
  if (typeof record.returnAfterSignIn !== 'boolean') return null;
  const entry: ConnectEntry = { handle: record.handle, savedAt: record.savedAt, returnAfterSignIn: record.returnAfterSignIn };
  return isFresh(entry) ? entry : null;
}

/** Returns false when the removal failed or storage is unavailable. */
function removeStored(): boolean {
  const store = storage();
  if (!store) return false;
  try { store.removeItem(STORAGE_KEY); return true; } catch { return false; }
}

function parseJson(raw: string): unknown {
  try { return JSON.parse(raw); } catch { return null; }
}

/** Storage is trusted only when it can be read; `null` means no usable entry is stored. */
function readStored(): { trusted: false } | { trusted: true; entry: ConnectEntry | null } {
  const store = storage();
  if (!store) return { trusted: false };
  let raw: string | null;
  try { raw = store.getItem(STORAGE_KEY); } catch { return { trusted: false }; }
  if (raw === null) return { trusted: true, entry: null };
  const entry = validateStored(parseJson(raw));
  if (entry) return { trusted: true, entry };
  // An invalid record is removed so it is never considered again; if that fails, storage is not trusted.
  return removeStored() ? { trusted: true, entry: null } : { trusted: false };
}

/** The one read path. Expired memory is dropped so it can never resurface. */
export function readConnectEntry(): ConnectEntry | null {
  if (!memoryAuthoritative) {
    const stored = readStored();
    if (stored.trusted) {
      memoryEntry = stored.entry;
      return stored.entry;
    }
    memoryAuthoritative = true;
  }
  if (memoryEntry && !isFresh(memoryEntry)) memoryEntry = null;
  return memoryEntry;
}

function writeEntry(entry: ConnectEntry): void {
  memoryEntry = entry;
  const store = storage();
  if (!store) { memoryAuthoritative = true; return; }
  try {
    store.setItem(STORAGE_KEY, JSON.stringify(entry));
    memoryAuthoritative = false;
  } catch {
    memoryAuthoritative = true;
  }
}

/** A new fragment replaces whatever was stored; the sign-in return flag starts false. */
export function saveConnectHandle(handle: string): void {
  if (!isConnectHandle(handle)) return;
  writeEntry({ handle, savedAt: clock(), returnAfterSignIn: false });
}

/** Removes the entry. A failed removal keeps memory authoritative with an empty record, so the stored copy is ignored. */
export function clearConnectEntry(): void {
  memoryEntry = null;
  memoryAuthoritative = !removeStored();
}

/** Records that the next sign-in should return to /connect. No-op without a fresh entry. */
export function markConnectReturn(): void {
  const entry = readConnectEntry();
  if (!entry) return;
  writeEntry({ ...entry, returnAfterSignIn: true });
}

/** True only once per flag: the handle stays so the consent screen can load, the return is consumed. */
export function takeConnectReturn(): boolean {
  const entry = readConnectEntry();
  if (!entry?.returnAfterSignIn) return false;
  writeEntry({ ...entry, returnAfterSignIn: false });
  return true;
}
