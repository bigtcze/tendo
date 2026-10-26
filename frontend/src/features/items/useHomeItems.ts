import { useCallback, useEffect, useRef, useState } from 'react';
import { getItemsSummary, listItemGroup, type ItemGroup, type ItemSummary } from './itemsApi';
import { HOME_GROUP_ROW_CAP, type GroupData } from './homeGroups';
import { emptyGroups, mergeGroupPage, fetchHomeGroups, isShowMoreBlocked } from './homeItemsHelpers';
import { useShowMoreFocus } from './useShowMoreFocus';

type HomeItemGroups = Record<ItemGroup, GroupData>;
type HomeItemsOptions = { householdId: string; mounted: { current: boolean }; onSignedOut: () => void };

export function useHomeItems({ householdId, mounted, onSignedOut }: HomeItemsOptions) {
  const [groupData, setGroupData] = useState<Record<ItemGroup, GroupData>>(emptyGroups);
  const [summary, setSummary] = useState<ItemSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [itemsRequestComplete, setItemsRequestComplete] = useState(false);
  const [error, setError] = useState(false);
  const dataHousehold = useRef<string | null>(null);
  const generation = useRef(0);
  const memberLoadGenerationRef = useRef(0);
  const sectionHeadingRefs = useRef(new Map<ItemGroup, HTMLHeadingElement>());
  const showMoreButtons = useRef(new Map<ItemGroup, HTMLButtonElement>());
  const showMoreInFlight = useRef(new Set<ItemGroup>());
  const focusedMoreGroup = useRef<ItemGroup | null>(null);

  const loadHome = useCallback(async ({ allowStaleRestart }: { allowStaleRestart: boolean }) => {
    const started = ++generation.current;
    const current = () => mounted.current && started === generation.current;
    setLoading(true);
    setGroupData((old) => Object.fromEntries(
      Object.entries(old).map(([key, value]) => [key, { ...value, loadingMore: false }]),
    ) as HomeItemGroups);
    showMoreInFlight.current.clear();
    async function load(allowRestart: boolean): Promise<void> {
      const summaryResult = await getItemsSummary(householdId);
      if (!current()) return;
      if (summaryResult.kind === 'unauthenticated') { if (!current()) return; onSignedOut(); setLoading(false); return; }
      if (summaryResult.kind !== 'ok') { setError(true); setLoading(false); return; }
      const nextSummary = summaryResult.summary;
      const entries = await fetchHomeGroups(householdId, nextSummary);
      if (!current()) return;
      if (entries.some(([, result]) => result.kind === 'unauthenticated')) { if (!current()) return; onSignedOut(); setLoading(false); return; }
      if (entries.some(([, result]) => result.kind === 'stale')) { if (allowRestart) return load(false); setError(true); setLoading(false); return; }
      if (entries.some(([, result]) => result.kind !== 'ok')) { setError(true); setLoading(false); return; }
      const nextData = emptyGroups();
      for (const [id, result] of entries) if (result.kind === 'ok') nextData[id] = {
        items: result.items.slice(0, HOME_GROUP_ROW_CAP), cursor: result.nextCursor, loadingMore: false,
        overflow: result.items.length > HOME_GROUP_ROW_CAP,
      };
      dataHousehold.current = householdId; setSummary(nextSummary);
      setGroupData(nextData); setError(false); setLoading(false);
    }
    await load(allowStaleRestart); if (current()) setItemsRequestComplete(true);
  }, [householdId, onSignedOut, mounted]);
  const refresh = useCallback(() => loadHome({ allowStaleRestart: true }), [loadHome]);

  useEffect(() => {
    memberLoadGenerationRef.current++;
    const generationRef = generation;
    const inFlightRef = showMoreInFlight;
    return () => { generationRef.current++; inFlightRef.current.clear(); };
  }, []);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- start async loading; state updates follow network responses.
    void loadHome({ allowStaleRestart: true });
  }, [loadHome]);

  async function showMore(id: ItemGroup) {
    const state = groupData[id];
    if (isShowMoreBlocked(state, showMoreInFlight.current.has(id), Boolean(summary), mounted.current)) return;
    const started = generation.current;
    const currentSummary = summary;
    const cursor = state.cursor;
    if (!currentSummary || !cursor) return;
    const focused = document.activeElement === showMoreButtons.current.get(id);
    showMoreInFlight.current.add(id);
    focusedMoreGroup.current = focused ? id : null;
    setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: true } }));
    const result = await listItemGroup(householdId, id, currentSummary.evaluatedOn, cursor);
    if (!mounted.current || started !== generation.current) return;
    showMoreInFlight.current.delete(id);
    if (result.kind === 'unauthenticated') { onSignedOut(); setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: false } })); }
    else if (result.kind === 'stale') { focusedMoreGroup.current = null; await loadHome({ allowStaleRestart: false }); }
    else if (result.kind === 'ok') {
      setGroupData((current) => {
        return { ...current, [id]: mergeGroupPage(current[id], result) };
      });
    } else { setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: false } })); setError(true); }
    if (focused && mounted.current && started === generation.current && result.kind === 'ok') focusedMoreGroup.current = id;
  }

  useShowMoreFocus(groupData, focusedMoreGroup, showMoreButtons, sectionHeadingRefs);
  return { groupData, summary, loading, error, itemsRequestComplete, dataHousehold,
    refresh, loadHome, showMore, invalidate: () => { generation.current++; },
    sectionHeadingRefs, showMoreButtons, memberLoadGeneration: memberLoadGenerationRef };
}
