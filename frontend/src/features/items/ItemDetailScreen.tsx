import { useEffect, useRef } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { useI18n, type Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Item } from './itemsApi';
import { ItemDetailActions } from './ItemDetailActions';
import { ItemDetailHistory } from './ItemDetailHistory';
import { ItemDetailLoadStates } from './ItemDetailLoadStates';
import { ItemDetailNotice } from './ItemDetailNotice';
import { ItemDetailReadView } from './ItemDetailReadView';
import { ItemEditSection } from './ItemEditSection';
import { useItemDetail } from './useItemDetail';
import { useItemDetailActions } from './useItemDetailActions';
import { useDetailCompletion } from './useDetailCompletion';
import { useItemDetailBusy } from './useItemDetailBusy';
import { useItemDetailEdit } from './useItemDetailEdit';
import { useItemDetailHistory } from './useItemDetailHistory';
import { useLoadGeneration } from './useLoadGeneration';

type Props = { householdId: string; itemId: string; login: string; onBack: () => void; onSignedOut: () => void; embedded?: boolean };
const BACK_LINK_CLASS = '-ml-3 inline-flex min-h-11 max-w-full whitespace-normal break-words text-left items-center rounded-xl px-3 text-muted '
  + 'hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

/** Detail opened from All items keeps an exact "Back to all items" link to the filtered page it came from. */
function BackToAllItems({ onBack, t }: { onBack: () => void; t: Translate }) {
  const returnTo = window.history.state?.returnTo;
  if (typeof returnTo !== 'string' || !returnTo.startsWith('/items')) return null;
  return <NavLink href={returnTo} onNavigate={onBack} className={BACK_LINK_CLASS}>{t('itemDetail.backAllItems')}</NavLink>;
}

function DetailEditDialog({ editing, subjects, members, busy, locale, t, onSubmit, onCancel }: {
  editing: NonNullable<ReturnType<typeof useItemDetail>['editing']>;
  subjects: Parameters<typeof ItemEditSection>[0]['subjects']; members: Parameters<typeof ItemEditSection>[0]['members'];
  busy: boolean; locale: string; t: Translate;
  onSubmit: Parameters<typeof ItemEditSection>[0]['onSubmit']; onCancel: () => void;
}) {
  return <div className="item-detail-edit-modal" role="dialog" aria-modal="true" aria-label={t('itemDetail.edit.formLabel')} onMouseDown={(event) => { if (event.target === event.currentTarget) onCancel(); }} onKeyDown={(event) => {
    if (event.key === 'Escape') { event.preventDefault(); onCancel(); return; }
    if (event.key !== 'Tab') return;
    const controls = [...event.currentTarget.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')];
    const index = controls.indexOf(document.activeElement as HTMLElement);
    if (event.shiftKey && index <= 0) { event.preventDefault(); controls.at(-1)?.focus(); }
    else if (!event.shiftKey && index === controls.length - 1) { event.preventDefault(); controls[0]?.focus(); }
  }}><div className="item-detail-edit-dialog"><ItemEditSection editing={editing} subjects={subjects} members={members} busy={busy} locale={locale} t={t} onSubmit={onSubmit} onCancel={onCancel} /></div></div>;
}

function responsibleLine(item: Item, members: Member[], t: Translate) {
  if (!item.responsibleUserId) return null;
  const name = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  return <p className="text-sm text-muted">{t('items.responsible.read', { name })}</p>;
}

export function ItemDetailScreen({ householdId, itemId, login, onBack, onSignedOut, embedded = false }: Props) {
  const { t, locale } = useI18n();
  const loads = useLoadGeneration();
  const historySectionRef = useRef<HTMLElement>(null);
  const historyHeadingRef = useRef<HTMLHeadingElement>(null);
  const history = useItemDetailHistory({
    headingRef: historyHeadingRef, sectionRef: historySectionRef, householdId, itemId, onSignedOut, current: loads.current, liveToken: loads.live,
  });
  const detail = useItemDetail({ householdId, itemId, onSignedOut, loads, history });
  const busy = useItemDetailBusy();
  const returnEditFocusRef = useRef(false);
  const edit = useItemDetailEdit({ householdId, itemId, onSignedOut, t, detail, busy, loads, returnEditFocusRef });
  const actions = useItemDetailActions({ householdId, itemId, onSignedOut, detail, busy, loads });
  const { state, item, notice, editing, editLoading, focusAfterMutationRef } = detail;
  const completion = useDetailCompletion(householdId, itemId, item?.title ?? '', t, onSignedOut, () => { void detail.retry(); });
  const editButtonRef = useRef<HTMLButtonElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    if (!editing) return;
    const modal = document.querySelector<HTMLElement>('.item-detail-edit-modal');
    modal?.querySelector<HTMLElement>('input:not([disabled]), select:not([disabled]), button:not([disabled])')?.focus();
  }, [editing]);

  // Declaration order matters: a mutation notice, then the save notice, then the return to Edit.
  useEffect(() => {
    if (focusAfterMutationRef.current && notice) { noticeRef.current?.focus(); focusAfterMutationRef.current = false; }
  }, [notice, focusAfterMutationRef]);
  useEffect(() => { if (notice === 'itemDetail.updated') { noticeRef.current?.focus(); returnEditFocusRef.current = false; } }, [notice]);
  useEffect(() => {
    if (!editing && !editLoading && !busy.busy && returnEditFocusRef.current) { editButtonRef.current?.focus(); returnEditFocusRef.current = false; }
  }, [editing, editLoading, busy.busy]);

  return <Layout embedded={embedded} actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle item-detail" data-item-detail>
    {embedded ? <h2 className="item-pane-heading font-display text-2xl">{t('itemDetail.selectedItem')}</h2> : null}
    {embedded ? <button type="button" className="item-pane-close" aria-label={t('itemDetail.closePane')} onClick={onBack}>× <span>{t('itemDetail.closePane')}</span></button> : <BackToAllItems onBack={onBack} t={t} />}
    <ItemDetailLoadStates state={state} t={t} onRetry={detail.retry} />
    {state === 'ready' && item ? <>
      <Heading className="mt-2 min-w-0 break-words font-display text-3xl leading-tight sm:text-4xl">{item.title}</Heading>
      <p className="mt-2 text-muted">{detail.subjectOverride ?? (detail.subjectName || t('items.subject.unknown'))}</p>
      {responsibleLine(item, detail.members, t)}
      {item.archived ? <p className="mt-5 rounded-xl bg-sand px-4 py-3">{t('itemDetail.archived')}</p> : null}
      <ItemDetailNotice notice={notice} undoArchive={detail.undoArchive} busy={busy.busy || completion.busy} t={t} focusRef={noticeRef} onUndo={actions.restore} />
      {editing
        ? <DetailEditDialog editing={editing} subjects={edit.subjects} members={edit.members} busy={busy.busy} locale={locale} t={t} onSubmit={edit.saveEdit} onCancel={edit.cancelEdit} />
        : <ItemDetailReadState item={item} detail={detail} busy={busy} completion={completion} actions={actions} edit={edit} editLoading={editLoading} editButtonRef={editButtonRef} history={history} historySectionRef={historySectionRef} historyHeadingRef={historyHeadingRef} t={t} locale={locale} />}
    </> : null}
  </section></Layout>;
}

function ItemDetailReadState({ item, detail, busy, completion, actions, edit, editLoading, editButtonRef, history, historySectionRef, historyHeadingRef, t, locale }: {
  item: Item; detail: ReturnType<typeof useItemDetail>; busy: ReturnType<typeof useItemDetailBusy>; completion: ReturnType<typeof useDetailCompletion>;
  actions: ReturnType<typeof useItemDetailActions>; edit: ReturnType<typeof useItemDetailEdit>; editLoading: boolean; editButtonRef: React.RefObject<HTMLButtonElement | null>;
  history: ReturnType<typeof useItemDetailHistory>; historySectionRef: React.RefObject<HTMLElement | null>; historyHeadingRef: React.RefObject<HTMLHeadingElement | null>;
  t: Translate; locale: string;
}) {
  return <>
    <ItemDetailReadView item={item} t={t} locale={locale} subjectName={detail.subjectOverride ?? (detail.subjectName || t('items.subject.unknown'))} />
    <div className="item-detail-actions">
      <ItemDetailActions item={item} busy={busy.busy || completion.busy} editLoading={editLoading} editRef={editButtonRef} t={t}
        onEdit={() => void edit.beginEdit()} onWorkflow={actions.setWorkflow} onArchive={actions.archive} onRestore={actions.restore}
        onDone={() => void completion.onDone()} onUndo={() => void completion.onUndo()} completionNotice={completion.notice} />
    </div>
    <ItemDetailHistory history={history} sectionRef={historySectionRef} headingRef={historyHeadingRef} t={t} locale={locale} />
  </>;
}
