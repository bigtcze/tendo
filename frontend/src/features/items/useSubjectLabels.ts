import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listSubjectsByIDs, type Subject } from './itemsApi';

type LabelState = { kind: 'queued' | 'loading' | 'missing' | 'failed' } | { kind: 'found'; subject: Subject };
type Controller = {
  scope: string;
  states: Map<string, LabelState>;
  queue: string[];
  running: boolean;
};

function makeController(scope: string): Controller {
  return { scope, states: new Map(), queue: [], running: false };
}

/** Per-screen, household-scoped ID label requests. */
export function useSubjectLabels(userId: string, householdId: string, visibleIDs: string[], onSignedOut: () => void) {
  const scope = `${userId}:${householdId}`;
  const controller = useMemo(() => makeController(scope), [scope]);
  const current = useRef<Controller | null>(null);
  const [, setRevision] = useState(0);
  const visibleKey = useMemo(() => [...new Set(visibleIDs)].sort().join('\u0000'), [visibleIDs]);

  useEffect(() => {
    current.current = controller;
    return () => { if (current.current === controller) current.current = null; };
  }, [controller]);

  const publish = useCallback((target: Controller) => {
    if (current.current === target) setRevision((revision) => revision + 1);
  }, []);

  const run = useCallback(async (target: Controller) => {
    if (current.current !== target || target.running) return;
    target.running = true;
    while (current.current === target) {
      if (!target.queue.length) break;
      const batch = target.queue.splice(0, Math.min(100, target.queue.length));
      for (const id of batch) target.states.set(id, { kind: 'loading' });
      publish(target);
      const [active, archived] = await Promise.all([
        listSubjectsByIDs(householdId, batch, false),
        listSubjectsByIDs(householdId, batch, true),
      ]);
      // Ownership is checked before touching shared bookkeeping after an await.
      if (current.current !== target) return;
      if (active.kind === 'unauthenticated' || archived.kind === 'unauthenticated') {
        target.running = false;
        onSignedOut();
        return;
      }
      const activeItems = active.kind === 'ok' ? active.subjects : [];
      const archivedItems = archived.kind === 'ok' ? archived.subjects : [];
      const returned = new Map<string, Subject>();
      for (const subject of [...activeItems, ...archivedItems]) returned.set(subject.id, subject);
      for (const id of batch) {
        const found = returned.get(id);
        if (found) target.states.set(id, { kind: 'found', subject: found });
        else if (active.kind === 'ok' && archived.kind === 'ok') target.states.set(id, { kind: 'missing' });
        else target.states.set(id, { kind: 'failed' });
      }
      // Publish every batch; IDs appended while requests are in flight run next.
      publish(target);
    }
    if (current.current === target) target.running = false;
  }, [householdId, onSignedOut, publish]);

  useEffect(() => {
    if (current.current !== controller) return;
    for (const id of visibleKey ? visibleKey.split('\u0000') : []) {
      if (controller.states.has(id)) continue;
      controller.states.set(id, { kind: 'queued' });
      controller.queue.push(id);
    }
    if (controller.queue.length && !controller.running) void run(controller);
  }, [controller, visibleKey, run]);

  const retry = useCallback(() => {
    if (current.current !== controller) return;
    const visible = new Set(visibleKey ? visibleKey.split('\u0000') : []);
    for (const id of visible) {
      const state = controller.states.get(id);
      if (state?.kind !== 'failed' && state?.kind !== 'missing') continue;
      controller.states.set(id, { kind: 'queued' });
      controller.queue.push(id);
    }
    publish(controller);
    if (controller.queue.length && !controller.running) void run(controller);
  }, [controller, publish, run, visibleKey]);

  const names = new Map<string, string>();
  let loading = false;
  let hasFailures = false;
  for (const id of visibleKey ? visibleKey.split('\u0000') : []) {
    const state = controller.scope === scope ? controller.states.get(id) : undefined;
    if (state?.kind === 'found') names.set(id, state.subject.name);
    if (state?.kind === 'queued' || state?.kind === 'loading') loading = true;
    if (state?.kind === 'failed' || state?.kind === 'missing') hasFailures = true;
  }
  const stateFor = (id: string) => controller.scope === scope ? controller.states.get(id)?.kind : undefined;
  return { names, loading, hasFailures, retry, stateFor };
}
