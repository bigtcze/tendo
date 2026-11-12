import type { Ref } from 'react';
import { Heading } from '../../app/Heading';
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

function responsibleLabel(item: Item, member: { userId: string; login: string } | undefined, viewerId: string, t: Translate) {
  if (!item.responsibleUserId) return null;
  if (!member) return t('items.responsible.unknown');
  return member.userId === viewerId ? t('items.assignee.you') : member.login;
}

/** The item's real state, written out and paired with its icon so colour is never the only signal. */
function StatePill({ item, t }: { item: Item; t: Translate }) {
  const state = itemState(item);
  return <p data-item-state data-item-state-kind={state}
    className={`inline-flex shrink-0 items-center gap-2 self-start rounded-full border px-3 py-1 text-small font-semibold ${itemStateTone[state].pill}`}>
    <ItemStateIcon state={state} className="size-4 shrink-0" />
    {t(itemStateLabel(item))}
  </p>;
}

export function ItemDetailReady({ item, subjectOverride, subjectName, detail, busy, edit, history, locale, viewerId, t, refs, onReload, actions }: {
  item: Item;
  subjectOverride: string | null;
  subjectName: string;
  detail: ItemDetailState;
  busy: ItemDetailBusy;
  edit: ReturnType<typeof useItemDetailEdit>;
  history: ItemDetailHistoryState;
  locale: string;
  viewerId: string;
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
  const responsibleName = responsibleLabel(item, responsibleMember, viewerId, t);

  return <>
    <header className="mt-3 min-w-0">
      <Heading className="min-w-0 text-pretty text-[1.75rem] font-semibold leading-[1.15] text-ink [overflow-wrap:anywhere] sm:text-[2rem]">{item.title}</Heading>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        <StatePill item={item} t={t} />
        <span className="min-w-0 text-small text-muted [overflow-wrap:anywhere]">
          {subjectOverride ?? (subjectName || t('items.subject.unknown'))}
        </span>
      </div>
    </header>
    <ItemDetailNotice notice={notice} undoArchive={undoArchive} busy={busy.busy} t={t} focusRef={refs.notice} onUndo={actions.onRestore} />
    {item.archived ? <p className="mt-4 rounded-lg border border-line bg-sand px-4 py-3 text-body">{t('itemDetail.archived')}</p> : null}
    {item.done ? <p className="mt-4 text-small text-muted">{t('itemDetail.done')}</p> : null}
    {editing
      ? <ItemEditSection editing={editing} subjects={edit.subjects} members={edit.members} busy={busy.busy} locale={locale} t={t}
        onSubmit={edit.saveEdit} onCancel={edit.cancelEdit} />
      : <>
        <ItemDetailReadView item={item} t={t} locale={locale} responsibleName={responsibleName} />
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
