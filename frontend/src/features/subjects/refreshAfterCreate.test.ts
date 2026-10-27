import type { SetStateAction } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';
import type { SubmitOutcome } from './SubjectForm';
import type { Subject } from './subjectsApi';
import type { ListState } from './subjectsModel';
import { refreshAfterCreate } from './refreshAfterCreate';

const listKey = `GET /api/v1/households/${HOUSEHOLD_ID}/subjects`;

const created: Subject = {
  id: 's-3',
  name: 'New subject',
  type: 'person',
  linkedUserId: null,
  archived: false,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

function context(onSetList: (list: ListState) => void = () => {}) {
  const events: string[] = [];
  const done: SubmitOutcome = { kind: 'done' };
  const ctx = {
    householdId: HOUSEHOLD_ID,
    subject: created,
    generation: { current: 0 },
    form: 1,
    finishAdd: vi.fn<(form: number, subject: Subject) => SubmitOutcome>(() => {
      events.push('finishAdd');
      return done;
    }),
    setLoadingMore: vi.fn(),
    pagingFocus: { current: null },
    pageLoad: { current: 0 },
    setCursorStack: vi.fn(),
    setCurrentCursor: vi.fn(),
    setList: vi.fn((action: SetStateAction<ListState>) => {
      events.push('setList');
      if (typeof action !== 'function') onSetList(action);
    }),
    onSignedOut: vi.fn(),
  };
  return { ctx, events, done };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('refreshAfterCreate', () => {
  it('finishes the add in the same continuation that applies the refreshed list', async () => {
    installFakeServer({ [listKey]: json(200, { items: [], nextCursor: null }) });
    // Work queued after the list is applied (for example a view switch) must not run before the
    // add finishes, or finishing would restore a notice and focus the switch already cleared.
    let laterWorkRan = false;
    const { ctx, events, done } = context(() => queueMicrotask(() => { laterWorkRan = true; }));
    ctx.finishAdd.mockImplementation(() => {
      events.push(laterWorkRan ? 'finishAdd:late' : 'finishAdd');
      return done;
    });

    await expect(refreshAfterCreate(ctx)).resolves.toBe(done);

    expect(events).toEqual(['setList', 'finishAdd']);
    expect(ctx.setList).toHaveBeenCalledWith({ kind: 'ready', items: [created], nextCursor: null });
    expect(ctx.finishAdd).toHaveBeenCalledWith(1, created);
  });

  it('finishes the add without touching the list when a view switch superseded the refresh', async () => {
    const { ctx, events, done } = context();
    installFakeServer({
      [listKey]: () => {
        ctx.generation.current += 1;
        return json(200, { items: [], nextCursor: null });
      },
    });

    await expect(refreshAfterCreate(ctx)).resolves.toBe(done);

    expect(events).toEqual(['finishAdd']);
    expect(ctx.setList).not.toHaveBeenCalled();
  });

  it('signs out instead of finishing when the refresh is unauthenticated', async () => {
    installFakeServer({ [listKey]: json(401, { title: 'x' }) });
    const { ctx } = context();

    await expect(refreshAfterCreate(ctx)).resolves.toEqual({ kind: 'handled' });

    expect(ctx.onSignedOut).toHaveBeenCalledOnce();
    expect(ctx.finishAdd).not.toHaveBeenCalled();
    expect(ctx.setList).not.toHaveBeenCalled();
  });
});
