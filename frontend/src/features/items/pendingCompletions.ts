const memory = new Map<string, string>();
const memoryAuthoritative = new Set<string>();
let storageUntrusted = false;
const prefix = 'tendo.pendingCompletion:';
function storage() { try { return globalThis.sessionStorage; } catch { return undefined; } }
export function completionAttemptKey(userId: string, householdId: string, itemId: string): string {
  return `${userId}:${householdId}:${itemId}`;
}
export function getPendingCompletion(userId: string, householdId: string, itemId: string): string | undefined {
  const id = completionAttemptKey(userId, householdId, itemId);
  if (memoryAuthoritative.has(id) || storageUntrusted) return memory.get(id);
  try {
    const value = storage()?.getItem(`${prefix}${id}`);
    if (value) { memory.set(id, value); return value; }
  } catch { /* use memory fallback */ }
  return memory.get(id);
}
export function setPendingCompletion(userId: string, householdId: string, itemId: string, key: string): void {
  const id = completionAttemptKey(userId, householdId, itemId);
  memory.set(id, key);
  const store = storage();
  if (!store) { memoryAuthoritative.add(id); return; }
  try { store.setItem(`${prefix}${id}`, key); memoryAuthoritative.delete(id); }
  catch { memoryAuthoritative.add(id); }
}
export function clearPendingCompletion(userId: string, householdId: string, itemId: string, key: string): void {
  const id = completionAttemptKey(userId, householdId, itemId);
  const pending = memoryAuthoritative.has(id) ? memory.get(id) : getPendingCompletion(userId, householdId, itemId);
  if (pending !== key) return;
  memory.delete(id);
  // The empty memory record stays authoritative so failed removal cannot resurrect the key.
  memoryAuthoritative.add(id);
  try { storage()?.removeItem(`${prefix}${id}`); } catch { /* memory stays authoritative */ }
}
export function clearPendingCompletions(): void {
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
      storageUntrusted = true;
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
