import type { Ref } from 'react';
import { Heading } from '../../app/Heading';
import { Eyebrow } from '../../components/ui/eyebrow';
import type { Translate } from '../../i18n';
import type { Item } from './itemsApi';
import { ItemDetailActions } from './ItemDetailActions';
import { ItemDetailHistory } from './ItemDetailHistory';
import { ItemDetailNotice } from './ItemDetailNotice';
import { ItemDetailReadView } from './ItemDetailReadView';
import { ItemEditSection } from './ItemEditSection';
import { ItemStateIcon, itemState, itemStateLabel, itemStateTone } from './itemStateTone';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailHistoryState } from './useItemDetailHistory';
import type { ItemDetailState } from './useItemDetail';
import type { WorkflowState } from './useItemDetailActions';
import type { useItemDetailEdit } from './useItemDetailEdit';

/**
 * The item's real state, written out and paired with its icon so colour is never the only signal.
 * Direction B: an upright caption-sized label, not a rounded pill, so the header stays a desk-like
 * title line and the status reads as data next to it.
 */
function StateLabel({ item, t }: { item: Item; t: Translate }) {
  const state = itemState(item);
  return <p data-item-state data-item-state-kind={state}
    className={`inline-flex shrink-0 items-center gap-1.5 self-start rounded-md border px-2 py-1 text-caption font-semibold uppercase tracking-[0.04em] ${itemStateTone[state].pill}`}>
    <ItemStateIcon state={state} className="size-3.5 shrink-0" />
    {t(itemStateLabel(item))}
  </p>;
}

export function ItemDetailReady({ item, subjectOverride, subjectName, detail, busy, edit, history, locale, t, refs, onReload, actions }: {
  item: Item;
  subjectOverride: string | null;
  subjectName: string;
  detail: ItemDetailState;
  busy: ItemDetailBusy;
  edit: ReturnType<typeof useItemDetailEdit>;
  history: ItemDetailHistoryState;
  locale: string;
  t: Translate;
  refs: {
    notice: Ref<HTMLParagraphElement>;
    completionNotice: Ref<HTMLDivElement>;
    editButton: Ref<HTMLButtonElement>;
    historySection: Ref<HTMLElement>;
    historyHeading: Ref<HTMLHeadingElement>;
  };
  onReload: () => void;
  actions: {
    onEdit: () => void;
    onWorkflow: (state: WorkflowState) => void;
    onArchive: () => void;
    onRestore: () => void;
    onDone: () => void;
    onUndo: () => void;
    onRetry: () => void;
  };
}) {
  const { notice, undoArchive, members, editing, editLoading, completionNotice } = detail;
  const responsibleMember = item.responsibleUserId
    ? members.find((member) => member.userId === item.responsibleUserId)
    : undefined;
  const responsible = responsibleMember
    ? t('items.responsible.read', { name: responsibleMember.login })
    : item.responsibleUserId ? t('items.responsible.read', { name: t('items.responsible.unknown') }) : null;

  return <>
    <header className="mt-3 min-w-0">
      <Eyebrow className="[overflow-wrap:anywhere]">
        {subjectOverride ?? (subjectName || t('items.subject.unknown'))}
      </Eyebrow>
      <div className="mt-1.5 flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <Heading className="min-w-0 [overflow-wrap:anywhere] text-pretty text-h1 leading-tight text-ink">{item.title}</Heading>
        <StateLabel item={item} t={t} />
      </div>
    </header>
    <ItemDetailNotice notice={notice} undoArchive={undoArchive} busy={busy.busy} t={t} focusRef={refs.notice} onUndo={actions.onRestore} />
    {item.archived
      ? <p className="mt-3 rounded-lg border border-line bg-sand px-4 py-2.5 text-small text-muted">{t('itemDetail.archived')}</p>
      : null}
    {editing
      ? <ItemEditSection editing={editing} subjects={edit.subjects} members={edit.members} busy={busy.busy} locale={locale} t={t}
        onSubmit={edit.saveEdit} onCancel={edit.cancelEdit} />
      : <>
        <ItemDetailReadView item={item} t={t} locale={locale} responsible={responsible} />
        <ItemDetailActions
          item={item} busy={busy.busy} editLoading={editLoading} editRef={refs.editButton} t={t}
          stale={detail.stale} onReload={onReload}
          onEdit={actions.onEdit} onWorkflow={actions.onWorkflow} onArchive={actions.onArchive} onRestore={actions.onRestore}
          onDone={actions.onDone}
          completionNotice={completionNotice?.text ?? null}
          undoAvailable={Boolean(completionNotice?.undoAvailable && completionNotice.completion?.id)}
          retryAvailable={Boolean(completionNotice?.retryKind)}
          completionNoticeRef={refs.completionNotice} onUndo={actions.onUndo} onRetry={actions.onRetry}
        />
        <ItemDetailHistory history={history} sectionRef={refs.historySection} headingRef={refs.historyHeading} t={t} locale={locale} />
      </>}
  </>;
}
