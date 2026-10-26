import type { MessageKey } from '../../i18n';
import type { Item, ItemGroup } from './itemsApi';

export const HOME_GROUP_ROW_CAP = 100;
export type GroupCount = 'needsAttention' | 'inProgress' | 'waiting' | 'upcoming' | 'paused';
export type GroupData = { items: Item[]; cursor: string | null; loadingMore: boolean; overflow: boolean };
export const groups: { id: ItemGroup; key: MessageKey; count: GroupCount; quiet?: boolean }[] = [
  { id: 'needs', key: 'items.group.needs', count: 'needsAttention' },
  { id: 'inProgress', key: 'items.group.inProgress', count: 'inProgress' },
  { id: 'waiting', key: 'items.group.waiting', count: 'waiting' },
  { id: 'upcoming', key: 'items.group.upcoming', count: 'upcoming', quiet: true },
  { id: 'paused', key: 'items.group.paused', count: 'paused', quiet: true },
];
