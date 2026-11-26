import type { ItemGroup } from './itemsApi';

type Tone = {
  /** Foreground for a written state or date on a plain surface. */
  text: string;
  pill: string;
};

/** Semantic pairs from the V4 token set; meaning is always also carried by the group name and icon. */
export const groupTone: Record<ItemGroup, Tone> = {
  needs: {
    text: 'text-needs-attention',
    pill: 'border-needs-attention/30 bg-needs-attention-bg text-needs-attention hover:border-needs-attention/60',
  },
  inProgress: {
    text: 'text-in-progress',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
  waiting: {
    text: 'text-waiting',
    pill: 'border-waiting/30 bg-waiting-bg text-waiting hover:border-waiting/60',
  },
  upcoming: {
    text: 'text-upcoming',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
  paused: {
    text: 'text-paused',
    pill: 'border-line bg-raised text-ink hover:border-control-border hover:bg-sand',
  },
};
