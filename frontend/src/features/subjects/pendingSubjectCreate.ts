import type { SubjectType } from './subjectsApi';

export type PendingSubjectCreate = { version: 1; key: string; body: { name: string; type: SubjectType; linkedUserId?: string } };
const memory = new Map<string, PendingSubjectCreate>();
const memoryAuthoritative = new Set<string>();
let storageUntrusted = false;
const prefix = 'tendo.pendingSubjectCreate.v1:';
function storage() { try { return globalThis.sessionStorage; } catch { return undefined; } }
function scope(userId: string, householdId: string) { return `${userId}:${householdId}`; }

export function getPendingSubjectCreate(userId: string, householdId: string): PendingSubjectCreate | undefined {
  const id = scope(userId, householdId);
  if (memoryAuthoritative.has(id) || storageUntrusted) return memory.get(id);
  try {
    const raw = storage()?.getItem(`${prefix}${id}`);
    if (raw) {
      const value: unknown = JSON.parse(raw);
      if (isPending(value)) { memory.set(id, value); return value; }
      memory.delete(id);
      try { storage()?.removeItem(`${prefix}${id}`); } catch { /* unavailable storage */ }
    }
  } catch { /* use memory fallback */ }
  return memory.get(id);
}

export function setPendingSubjectCreate(userId: string, householdId: string, pending: PendingSubjectCreate): void {
  const id = scope(userId, householdId);
  memory.set(id, pending);
  // Once persistence fails, memory is authoritative so stale stored records cannot replace newer attempts.
  const store = storage();
  if (!store) { memoryAuthoritative.add(id); return; }
  try { store.setItem(`${prefix}${id}`, JSON.stringify(pending)); memoryAuthoritative.delete(id); }
  catch { memoryAuthoritative.add(id); }
}

export function clearPendingSubjectCreate(userId: string, householdId: string, key: string): void {
  const id = scope(userId, householdId);
  const pending = memoryAuthoritative.has(id) ? memory.get(id) : getPendingSubjectCreate(userId, householdId);
  if (pending?.key !== key) return;
  memory.delete(id);
  // The empty memory record stays authoritative (a tombstone) so a failed removal cannot resurrect it.
  memoryAuthoritative.add(id);
  try { storage()?.removeItem(`${prefix}${id}`); } catch { /* memory stays authoritative */ }
}

export function clearPendingSubjectCreates(): void {
  for (const id of new Set([...memory.keys(), ...memoryAuthoritative])) {
    memory.delete(id);
    memoryAuthoritative.add(id);
  }
  storageUntrusted = true;
  try {
    const store = storage();
    if (!store) {
      memory.clear();
      memoryAuthoritative.clear();
      return;
    }
    for (let index = store.length - 1; index >= 0; index--) {
      const key = store.key(index);
      if (key?.startsWith(prefix)) store.removeItem(key);
    }
    memory.clear();
    memoryAuthoritative.clear();
    storageUntrusted = false;
  } catch {
    // Retain known authoritative state and distrust storage for unknown scopes.
    storageUntrusted = true;
  }
}

function isPending(value: unknown): value is PendingSubjectCreate {
  if (!value || typeof value !== 'object') return false;
  const record = value as Partial<PendingSubjectCreate>;
  return record.version === 1 && typeof record.key === 'string' && record.key.length > 0 &&
    !!record.body && typeof record.body.name === 'string' &&
    ['person', 'home', 'vehicle', 'pet', 'custom'].includes(record.body.type) &&
    (record.body.linkedUserId === undefined || typeof record.body.linkedUserId === 'string');
}
