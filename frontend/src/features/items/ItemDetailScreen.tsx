import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { useI18n, type Translate } from '../../i18n';
import { ItemDetailReady } from './ItemDetailReady';
import { ItemDetailLoadStates } from './ItemDetailLoadStates';
import { useLoadGeneration } from './useLoadGeneration';
import { useItemDetailScreenState } from './useItemDetailScreenState';
import { useItemDetailFocus } from './useItemDetailFocus';

type Props = { householdId: string; itemId: string; login: string; userId: string; onBack: () => void; onSignedOut: () => void };
const BACK_LINK_CLASS = '-ml-3 inline-flex min-h-11 max-w-full whitespace-normal break-words text-left items-center rounded-xl px-3 text-muted '
  + 'hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

/** Detail opened from All items keeps an exact return link; a deep link still offers the way home. */
function BackLink({ onBack, t }: { onBack: () => void; t: Translate }) {
  const returnTo = window.history.state?.returnTo;
  const fromAllItems = typeof returnTo === 'string' && returnTo.startsWith('/items');
  return <NavLink href={fromAllItems ? returnTo : '/'} onNavigate={onBack} className={BACK_LINK_CLASS}>
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75"
      strokeLinecap="round" strokeLinejoin="round" className="mr-1 size-4 shrink-0">
      <path d="M14 6l-6 6 6 6" />
    </svg>
    {fromAllItems ? t('itemDetail.backAllItems') : t('itemDetail.backHome')}
  </NavLink>;
}

export function ItemDetailScreen({ householdId, itemId, login, userId, onBack, onSignedOut }: Props) {
  const { t, locale } = useI18n();
  const loads = useLoadGeneration();
  const screen = useItemDetailScreenState({ householdId, itemId, userId, onSignedOut, t, loads });
  const { history, detail, busy, edit, actions, completion, focus } = screen;
  useItemDetailFocus({ ...focus, actions: { ...focus.actions, undoFocusRef: completion.focusAfterUndo } });
  const { state, item, subjectOverride, subjectName } = detail;

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle measure">
    <BackLink onBack={onBack} t={t} />
    <ItemDetailLoadStates state={state} t={t} onRetry={detail.retry} />
    {state === 'ready' && item ? <ItemDetailReady
      item={item} subjectOverride={subjectOverride} subjectName={subjectName} detail={detail} busy={busy} edit={edit} history={history} locale={locale} viewerId={userId} t={t}
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
