import type { SubjectType } from './subjectsApi';

export type PendingSubjectCreate = { version: 1; key: string; body: { name: string; type: SubjectType; linkedUserId?: string } };
const memory = new Map<string, PendingSubjectCreate>();
const prefix = 'tendo.pendingSubjectCreate.v1:';
function storage() { try { return globalThis.sessionStorage; } catch { return undefined; } }
function scope(userId: string, householdId: string) { return `${userId}:${householdId}`; }

export function getPendingSubjectCreate(userId: string, householdId: string): PendingSubjectCreate | undefined {
  const id = scope(userId, householdId);
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
  try { storage()?.setItem(`${prefix}${id}`, JSON.stringify(pending)); } catch { /* memory fallback */ }
}

export function clearPendingSubjectCreate(userId: string, householdId: string): void {
  const id = scope(userId, householdId);
  memory.delete(id);
  try { storage()?.removeItem(`${prefix}${id}`); } catch { /* memory fallback */ }
}

export function clearPendingSubjectCreates(): void {
  memory.clear();
  try {
    const store = storage();
    if (!store) return;
    for (let index = store.length - 1; index >= 0; index--) {
      const key = store.key(index);
      if (key?.startsWith(prefix)) store.removeItem(key);
    }
  } catch { /* ignore unavailable storage */ }
}

function isPending(value: unknown): value is PendingSubjectCreate {
  if (!value || typeof value !== 'object') return false;
  const record = value as Partial<PendingSubjectCreate>;
  return record.version === 1 && typeof record.key === 'string' && record.key.length > 0 &&
    !!record.body && typeof record.body.name === 'string' &&
    ['person', 'home', 'vehicle', 'pet', 'custom'].includes(record.body.type) &&
    (record.body.linkedUserId === undefined || typeof record.body.linkedUserId === 'string');
}
