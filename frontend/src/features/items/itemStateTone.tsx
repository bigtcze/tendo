import type { Item, ItemGroup } from './itemsApi';
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

type Tone = { tile: string; text: string; pill: string };

/**
 * V4 semantic pairs. Colour never carries the state alone: every use also renders the state in words and
 * with its icon, so the tile tint only reinforces what the row already says.
 */
export const itemStateTone: Record<ItemState, Tone> = {
  needs: {
    tile: 'bg-needs-attention-bg text-needs-attention',
    text: 'text-needs-attention',
    pill: 'border-needs-attention/30 bg-needs-attention-bg text-needs-attention',
  },
  inProgress: {
    tile: 'bg-in-progress-bg text-in-progress',
    text: 'text-in-progress',
    pill: 'border-in-progress/30 bg-in-progress-bg text-in-progress',
  },
  waiting: {
    tile: 'bg-waiting-bg text-waiting',
    text: 'text-waiting',
    pill: 'border-waiting/30 bg-waiting-bg text-waiting',
  },
  upcoming: {
    tile: 'bg-upcoming-bg text-upcoming',
    text: 'text-upcoming',
    pill: 'border-upcoming/30 bg-upcoming-bg text-upcoming',
  },
  paused: {
    tile: 'bg-paused-bg text-paused',
    text: 'text-paused',
    pill: 'border-line bg-paused-bg text-paused',
  },
  done: {
    tile: 'bg-done-bg text-done',
    text: 'text-done',
    pill: 'border-done/30 bg-done-bg text-done',
  },
  archived: {
    tile: 'bg-paused-bg text-paused',
    text: 'text-muted',
    pill: 'border-line bg-sand text-muted',
  },
};

export function ItemStateIcon({ state, className }: { state: ItemState; className?: string }) {
  if (state === 'done') return <CheckIcon className={className} />;
  if (state === 'archived') return <ArchiveIcon className={className} />;
  return <GroupStatusIcon group={state} className={className} />;
}
