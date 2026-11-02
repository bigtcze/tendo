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

/** Detail opened from All items keeps an exact "Back to all items" link to the filtered page it came from. */
function BackToAllItems({ onBack, t }: { onBack: () => void; t: Translate }) {
  const returnTo = window.history.state?.returnTo;
  if (typeof returnTo !== 'string' || !returnTo.startsWith('/items')) return null;
  return <NavLink href={returnTo} onNavigate={onBack} className={BACK_LINK_CLASS}>{t('itemDetail.backAllItems')}</NavLink>;
}

export function ItemDetailScreen({ householdId, itemId, login, userId, onBack, onSignedOut }: Props) {
  const { t, locale } = useI18n();
  const loads = useLoadGeneration();
  const screen = useItemDetailScreenState({ householdId, itemId, userId, onSignedOut, t, loads });
  const { history, detail, busy, edit, actions, focus } = screen;
  useItemDetailFocus(focus);
  const { state, item, subjectOverride, subjectName } = detail;

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <BackToAllItems onBack={onBack} t={t} />
    <ItemDetailLoadStates state={state} t={t} onRetry={detail.retry} />
    {state === 'ready' && item ? <ItemDetailReady
      item={item} subjectOverride={subjectOverride} subjectName={subjectName} detail={detail} busy={busy} edit={edit} history={history} locale={locale} t={t}
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
