import type { Ref } from 'react';
import { Heading } from '../../app/Heading';
import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Item } from './itemsApi';
import { ItemDetailActions } from './ItemDetailActions';
import { ItemDetailHistory } from './ItemDetailHistory';
import { ItemDetailNotice } from './ItemDetailNotice';
import { ItemDetailReadView } from './ItemDetailReadView';
import { ItemEditSection } from './ItemEditSection';
import { DETAIL_TITLE_CLASS } from './itemDetailStyles';
import { ItemStateIcon, itemState, itemStateLabel, itemStateTone } from './itemStateTone';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailHistoryState } from './useItemDetailHistory';
import type { ItemDetailState } from './useItemDetail';
import type { WorkflowState } from './useItemDetailActions';
import type { useItemDetailEdit } from './useItemDetailEdit';

/** The item's real state, written out and paired with its icon so colour is never the only signal. */
function StatePill({ item, t }: { item: Item; t: Translate }) {
  const state = itemState(item);
  return <p data-item-state data-item-state-kind={state}
    className={`inline-flex shrink-0 items-center gap-2 self-start rounded-full border px-3 py-1 text-small font-semibold ${itemStateTone[state].pill}`}>
    <ItemStateIcon state={state} className="size-4 shrink-0" />
    {t(itemStateLabel(item))}
  </p>;
}

/** The assigned member's name, or "Unknown member" when the account left the household. */
function responsibleName(item: Item, members: Member[], t: Translate) {
  if (!item.responsibleUserId) return null;
  return members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
}

/** One quiet line on what the page's state means; an archived item says that instead of the completion line. */
function StateSentence({ item, t }: { item: Item; t: Translate }) {
  const sentence = item.archived ? t('itemDetail.archived') : item.done ? t('itemDetail.done') : null;
  return sentence ? <p className="mt-4 max-w-prose text-muted">{sentence}</p> : null;
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
  const responsible = responsibleName(item, members, t);

  return <>
    <header className="mt-2 min-w-0">
      {/* The item's subject is a fact, so it stays — as a quiet contextual line, not an uppercase label. */}
      <p className="text-small text-muted [overflow-wrap:anywhere]">
        {subjectOverride ?? (subjectName || t('items.subject.unknown'))}
      </p>
      <div className="mt-2 flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
        <Heading className={DETAIL_TITLE_CLASS}>{item.title}</Heading>
        <StatePill item={item} t={t} />
      </div>
    </header>
    <ItemDetailNotice notice={notice} undoArchive={undoArchive} busy={busy.busy} t={t} focusRef={refs.notice} onUndo={actions.onRestore} />
    <StateSentence item={item} t={t} />
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
