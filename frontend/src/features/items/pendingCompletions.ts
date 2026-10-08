const memory = new Map<string, string>();
const prefix = 'tendo.pendingCompletion:';
function storage() { try { return globalThis.sessionStorage; } catch { return undefined; } }
export function completionAttemptKey(userId: string, householdId: string, itemId: string): string {
  return `${userId}:${householdId}:${itemId}`;
}
export function getPendingCompletion(userId: string, householdId: string, itemId: string): string | undefined {
  const id = completionAttemptKey(userId, householdId, itemId);
  try { const value = storage()?.getItem(`${prefix}${id}`); if (value) return value; } catch { /* use memory fallback */ }
  return memory.get(id);
}
export function setPendingCompletion(userId: string, householdId: string, itemId: string, key: string): void {
  const id = completionAttemptKey(userId, householdId, itemId);
  memory.set(id, key);
  try { storage()?.setItem(`${prefix}${id}`, key); } catch { /* memory fallback */ }
}
export function clearPendingCompletion(userId: string, householdId: string, itemId: string): void {
  const id = completionAttemptKey(userId, householdId, itemId);
  memory.delete(id);
  try { storage()?.removeItem(`${prefix}${id}`); } catch { /* memory fallback */ }
}
export function clearPendingCompletions(): void {
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
