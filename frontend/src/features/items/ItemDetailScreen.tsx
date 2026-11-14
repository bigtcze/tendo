import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { useI18n, type Translate } from '../../i18n';
import { ItemDetailReady } from './ItemDetailReady';
import { ItemDetailLoadStates } from './ItemDetailLoadStates';
import { DETAIL_BACK_LINK_CLASS } from './itemDetailStyles';
import { useLoadGeneration } from './useLoadGeneration';
import { useItemDetailScreenState } from './useItemDetailScreenState';
import { useItemDetailFocus } from './useItemDetailFocus';

type Props = { householdId: string; itemId: string; login: string; userId: string; onBack: () => void; onSignedOut: () => void };

/** The filtered All items page this detail was opened from, when it came from there. */
function allItemsReturnTo() {
  const returnTo = window.history.state?.returnTo;
  return typeof returnTo === 'string' && returnTo.startsWith('/items') ? returnTo : null;
}

/** Detail opened from All items keeps an exact "Back to all items" link to the filtered page it came from. */
function BackToAllItems({ onBack, t }: { onBack: () => void; t: Translate }) {
  const returnTo = allItemsReturnTo();
  if (!returnTo) return null;
  return <NavLink href={returnTo} onNavigate={onBack} className={DETAIL_BACK_LINK_CLASS}>{t('itemDetail.backAllItems')}</NavLink>;
}

export function ItemDetailScreen({ householdId, itemId, login, userId, onBack, onSignedOut }: Props) {
  const { t, locale } = useI18n();
  const loads = useLoadGeneration();
  const screen = useItemDetailScreenState({ householdId, itemId, userId, onSignedOut, t, loads });
  const { history, detail, busy, edit, actions, completion, focus } = screen;
  useItemDetailFocus({ ...focus, actions: { ...focus.actions, undoFocusRef: completion.focusAfterUndo } });
  const { state, item, subjectOverride, subjectName } = detail;

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <BackToAllItems onBack={onBack} t={t} />
    {/* The gone screen keeps a way out: the All items link when it came from there, the overview otherwise. */}
    <ItemDetailLoadStates state={state} t={t} onRetry={detail.retry} onOverview={allItemsReturnTo() ? null : onBack} />
    {state === 'ready' && item ? <ItemDetailReady
      item={item} subjectOverride={subjectOverride} subjectName={subjectName} detail={detail} busy={busy} edit={edit} history={history} locale={locale} t={t}
      onReload={screen.actions.reload}
      refs={{
        notice: screen.noticeRef,
        completionNotice: screen.completionNoticeRef,
        editButton: screen.editButtonRef,
        historySection: screen.historySectionRef,
        historyHeading: screen.historyHeadingRef,
      }}
      actions={{
        onEdit: () => void edit.beginEdit(),
        onWorkflow: actions.setWorkflow,
        onArchive: actions.archive,
        onRestore: actions.restore,
        onDone: actions.finish,
        onUndo: actions.undo,
        onRetry: actions.retryNotice,
      }}
    /> : null}
  </section></Layout>;
}
