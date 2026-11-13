import type { ItemGroup } from './itemsApi';

type Tone = {
  dot: string;
  tile: string;
  /** Foreground for a written state or date on a plain surface. */
  text: string;
  pill: string;
};

/** Semantic pairs from the V4 token set; meaning is always also carried by the group name and icon. */
export const groupTone: Record<ItemGroup, Tone> = {
  needs: {
    dot: 'bg-needs-attention',
    tile: 'bg-needs-attention-bg text-needs-attention',
    text: 'text-needs-attention',
    pill: 'border-needs-attention/30 bg-needs-attention-bg text-needs-attention hover:border-needs-attention/60',
  },
  inProgress: {
    dot: 'bg-in-progress',
    tile: 'bg-in-progress-bg text-in-progress',
    text: 'text-in-progress',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
  waiting: {
    dot: 'bg-waiting',
    tile: 'bg-waiting-bg text-waiting',
    text: 'text-waiting',
    pill: 'border-waiting/30 bg-waiting-bg text-waiting hover:border-waiting/60',
  },
  upcoming: {
    dot: 'bg-upcoming',
    tile: 'bg-upcoming-bg text-upcoming',
    text: 'text-upcoming',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
  paused: {
    dot: 'bg-paused',
    tile: 'bg-paused-bg text-paused',
    text: 'text-paused',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
};
