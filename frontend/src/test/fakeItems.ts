import type { Item, ItemSummary } from '../features/items/itemsApi';
import { json, type RecordedRequest } from './fakeServer';

export function itemRoutes(base: string, source: Item[] | (() => Item[]), options: { evaluatedOn?: string } = {}) {
  const evaluatedOn = options.evaluatedOn ?? '2026-01-15';
  const getItems = () => typeof source === 'function' ? source() : source;
  const attentionOf = (item: Item): Item['attention'] => item.attentionOn === null || item.attentionOn <= evaluatedOn ? 'needs_attention' : 'upcoming';
  const normalized = (value: string) => value.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLocaleLowerCase();
  const filteredForQuery = (request: RecordedRequest) => getItems()
    .filter((item) => !request.query.has('q') || normalized(`${item.title} ${item.notes ?? ''}`).includes(normalized(request.query.get('q')!)))
    .filter((item) => !request.query.has('subjectId') || item.subjectId === request.query.get('subjectId'))
    .filter((item) => !request.query.has('responsibleUserId') || (request.query.get('responsibleUserId') === 'unassigned' ? item.responsibleUserId === null : item.responsibleUserId === request.query.get('responsibleUserId')))
    .filter((item) => !request.query.has('recurring') || Boolean(item.recurrence) === (request.query.get('recurring') === 'true'));
  const countsFor = (request: RecordedRequest): ItemSummary => {
    const counts: ItemSummary = { evaluatedOn, total: 0, needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0 };
    for (const item of filteredForQuery(request)) {
      if (item.archived) counts.archived++;
      else if (item.done) counts.done++;
      else if (item.workflowState === 'paused') counts.paused++;
      else if (item.workflowState === 'in_progress') counts.inProgress++;
      else if (item.workflowState === 'waiting') counts.waiting++;
      else if (attentionOf(item) === 'needs_attention') counts.needsAttention++;
      else counts.upcoming++;
    }
    counts.total = counts.needsAttention + counts.inProgress + counts.waiting + counts.upcoming + counts.paused + counts.done + counts.archived;
    return counts;
  };
  return {
    [`GET ${base}/items/summary`]: (request: RecordedRequest) => json(200, countsFor(request)),
    [`GET ${base}/items`]: (request: RecordedRequest) => {
      const archived = request.query.get('archived') === 'true';
      const done = request.query.get('done') === 'true';
      let filtered = filteredForQuery(request)
        .filter((item) => item.archived === archived && item.done === done)
        .filter((item) => !request.query.has('workflowState') || item.workflowState === request.query.get('workflowState'))
        .filter((item) => !request.query.has('attention') || attentionOf(item) === request.query.get('attention'))
        .map((item) => ({ ...item, attention: attentionOf(item) }));
      if (request.query.get('sort') === 'attention_on') filtered = filtered.sort((a, b) => (a.attentionOn === null ? 0 : 1) - (b.attentionOn === null ? 0 : 1) || (a.attentionOn ?? '').localeCompare(b.attentionOn ?? '') || a.id.localeCompare(b.id));
      const limit = Number(request.query.get('limit') ?? 100);
      const offset = Number((request.query.get('cursor') ?? 'c:0').replace(/^c:/, ''));
      const page = filtered.slice(offset, offset + limit);
      return json(200, { items: page, nextCursor: offset + page.length < filtered.length ? `c:${offset + page.length}` : null });
    },
  };
}
