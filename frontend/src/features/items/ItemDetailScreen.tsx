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
import { useItemDetailBusy } from './useItemDetailBusy';
import { useItemDetailEdit } from './useItemDetailEdit';
import { useItemDetailHistory } from './useItemDetailHistory';
import { useLoadGeneration } from './useLoadGeneration';

type Props = { householdId: string; itemId: string; login: string; onBack: () => void; onSignedOut: () => void };
const BACK_LINK_CLASS = '-ml-3 inline-flex min-h-11 max-w-full whitespace-normal break-words text-left items-center rounded-xl px-3 text-muted '
  + 'hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

/** Detail opened from All items keeps an exact "Back to all items" link to the filtered page it came from. */
function BackToAllItems({ onBack, t }: { onBack: () => void; t: Translate }) {
  const returnTo = window.history.state?.returnTo;
  if (typeof returnTo !== 'string' || !returnTo.startsWith('/items')) return null;
  return <NavLink href={returnTo} onNavigate={onBack} className={BACK_LINK_CLASS}>{t('itemDetail.backAllItems')}</NavLink>;
}

function responsibleLine(item: Item, members: Member[], t: Translate) {
  if (!item.responsibleUserId) return null;
  const name = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  return <p className="text-sm text-muted">{t('items.responsible.read', { name })}</p>;
}

export function ItemDetailScreen({ householdId, itemId, login, onBack, onSignedOut }: Props) {
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
  const editButtonRef = useRef<HTMLButtonElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const { state, item, notice, editing, editLoading, focusAfterMutationRef } = detail;

  // Declaration order matters: a mutation notice, then the save notice, then the return to Edit.
  useEffect(() => {
    if (focusAfterMutationRef.current && notice) { noticeRef.current?.focus(); focusAfterMutationRef.current = false; }
  }, [notice, focusAfterMutationRef]);
  useEffect(() => { if (notice === 'itemDetail.updated') { noticeRef.current?.focus(); returnEditFocusRef.current = false; } }, [notice]);
  useEffect(() => {
    if (!editing && !editLoading && !busy.busy && returnEditFocusRef.current) { editButtonRef.current?.focus(); returnEditFocusRef.current = false; }
  }, [editing, editLoading, busy.busy]);

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <BackToAllItems onBack={onBack} t={t} />
    <ItemDetailLoadStates state={state} t={t} onRetry={detail.retry} />
    {state === 'ready' && item ? <>
      <Heading className="mt-2 min-w-0 break-words font-display text-3xl leading-tight sm:text-4xl">{item.title}</Heading>
      <p className="mt-2 text-muted">{detail.subjectOverride ?? (detail.subjectName || t('items.subject.unknown'))}</p>
      {responsibleLine(item, detail.members, t)}
      {item.archived ? <p className="mt-5 rounded-xl bg-sand px-4 py-3">{t('itemDetail.archived')}</p> : null}
      <ItemDetailNotice notice={notice} undoArchive={detail.undoArchive} busy={busy.busy} t={t} focusRef={noticeRef} onUndo={actions.restore} />
      {editing
        ? <ItemEditSection editing={editing} subjects={edit.subjects} members={edit.members} busy={busy.busy} locale={locale} t={t}
          onSubmit={edit.saveEdit} onCancel={edit.cancelEdit} />
        : <>
          <ItemDetailReadView item={item} t={t} locale={locale} />
          <ItemDetailActions item={item} busy={busy.busy} editLoading={editLoading} editRef={editButtonRef} t={t}
            onEdit={() => void edit.beginEdit()} onWorkflow={actions.setWorkflow} onArchive={actions.archive} onRestore={actions.restore} />
          <ItemDetailHistory history={history} sectionRef={historySectionRef} headingRef={historyHeadingRef} t={t} locale={locale} />
        </>}
    </> : null}
  </section></Layout>;
}
