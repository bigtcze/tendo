import type { MessageKey } from '../../i18n';
import type { Item, ItemGroup } from './itemsApi';
import { groupTone } from './homeGroupTone';
import { ArchiveIcon, CheckIcon, GroupStatusIcon } from './HomeIcons';

/** The real state a row or a detail header shows; archived is a listing state, not a workflow state. */
export type ItemState = ItemGroup | 'done' | 'archived';

export function itemState(item: Pick<Item, 'done' | 'archived' | 'workflowState' | 'attention'>): ItemState {
  if (item.archived) return 'archived';
  if (item.done) return 'done';
  if (item.workflowState === 'in_progress') return 'inProgress';
  if (item.workflowState === 'waiting') return 'waiting';
  if (item.workflowState === 'paused') return 'paused';
  return item.attention === 'upcoming' ? 'upcoming' : 'needs';
}

/**
 * One source of truth for a state's words, icon and V4 semantic pair: the five workflow states come from the
 * Home group tones, and only the two listing states All items and detail need are added here.
 */
export const itemStateLabelKey: Record<ItemState, MessageKey> = {
  needs: 'items.group.needs',
  inProgress: 'items.group.inProgress',
  waiting: 'items.group.waiting',
  upcoming: 'items.group.upcoming',
  paused: 'items.group.paused',
  done: 'itemDetail.state.done',
  archived: 'allItems.status.archived',
};

export const itemStateTone: Record<ItemState, (typeof groupTone)[ItemGroup]> = {
  ...groupTone,
  done: {
    dot: 'bg-done',
    tile: 'bg-done-bg text-done',
    text: 'text-done',
    pill: 'border-done/30 bg-done-bg text-done',
  },
  archived: {
    dot: 'bg-paused',
    tile: 'bg-paused-bg text-paused',
    text: 'text-muted',
    pill: 'border-line bg-sand text-muted',
  },
};

/** The written state and its tone always describe the same value. */
export function itemStateLabel(item: Pick<Item, 'done' | 'archived' | 'workflowState' | 'attention'>): MessageKey {
  return itemStateLabelKey[itemState(item)];
}

export function ItemStateIcon({ state, className }: { state: ItemState; className?: string }) {
  if (state === 'done') return <CheckIcon className={className} />;
  if (state === 'archived') return <ArchiveIcon className={className} />;
  return <GroupStatusIcon group={state} className={className} />;
}
