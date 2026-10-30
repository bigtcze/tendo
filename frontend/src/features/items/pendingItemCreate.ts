import type { components } from '../../../../api/generated/api';

type CreateItemRequest = components['schemas']['CreateItemRequest'];
export type PendingItemCreate = { version: 1; key: string; body: CreateItemRequest };
const memory = new Map<string, PendingItemCreate>();
const memoryAuthoritative = new Set<string>();
const prefix = 'tendo.pendingItemCreate.v1:';
function storage() { try { return globalThis.sessionStorage; } catch { return undefined; } }
function scope(userId: string, householdId: string) { return `${userId}:${householdId}`; }

export function getPendingItemCreate(userId: string, householdId: string): PendingItemCreate | undefined {
  const id = scope(userId, householdId);
  if (memoryAuthoritative.has(id)) return memory.get(id);
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

export function setPendingItemCreate(userId: string, householdId: string, pending: PendingItemCreate): void {
  const id = scope(userId, householdId);
  memory.set(id, pending);
  // Once persistence fails, memory is authoritative so stale stored records cannot replace newer attempts.
  const store = storage();
  if (!store) { memoryAuthoritative.add(id); return; }
  try { store.setItem(`${prefix}${id}`, JSON.stringify(pending)); memoryAuthoritative.delete(id); }
  catch { memoryAuthoritative.add(id); }
}

export function clearPendingItemCreate(userId: string, householdId: string, key: string): void {
  const id = scope(userId, householdId);
  const pending = memoryAuthoritative.has(id) ? memory.get(id) : getPendingItemCreate(userId, householdId);
  if (pending?.key !== key) return;
  memory.delete(id);
  // The empty memory record stays authoritative (a tombstone) so a failed removal cannot resurrect it.
  memoryAuthoritative.add(id);
  try { storage()?.removeItem(`${prefix}${id}`); } catch { /* memory stays authoritative */ }
}

export function clearPendingItemCreates(): void {
  memory.clear();
  memoryAuthoritative.clear();
  try {
    const store = storage();
    if (!store) return;
    for (let index = store.length - 1; index >= 0; index--) {
      const key = store.key(index);
      if (key?.startsWith(prefix)) store.removeItem(key);
    }
  } catch { /* ignore unavailable storage */ }
}

function isPending(value: unknown): value is PendingItemCreate {
  if (!value || typeof value !== 'object') return false;
  const record = value as Partial<PendingItemCreate>;
  const body = record.body;
  if (record.version !== 1 || typeof record.key !== 'string' || !/^[\x21-\x7e]{1,128}$/.test(record.key) || !body || typeof body !== 'object') return false;
  const allowed = ['title', 'subjectId', 'attentionOn', 'notes', 'responsibleUserId', 'historicalCompletedOn', 'recurrence'];
  if (Object.keys(body).some((key) => !allowed.includes(key))) return false;
  if (typeof body.title !== 'string' || typeof body.subjectId !== 'string') return false;
  if (body.attentionOn !== undefined && typeof body.attentionOn !== 'string') return false;
  if (body.notes !== undefined && typeof body.notes !== 'string') return false;
  if (body.responsibleUserId !== undefined && typeof body.responsibleUserId !== 'string') return false;
  if (body.historicalCompletedOn !== undefined && typeof body.historicalCompletedOn !== 'string') return false;
  if (body.recurrence !== null && body.recurrence !== undefined) {
    const recurrence = body.recurrence;
    if (!recurrence || typeof recurrence !== 'object' || !Number.isInteger(recurrence.intervalValue) || typeof recurrence.intervalUnit !== 'string' || !['day', 'week', 'month', 'year'].includes(recurrence.intervalUnit) || !['fixed', 'after_completion'].includes(recurrence.mode)) return false;
  }
  return true;
}
