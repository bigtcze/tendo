import { useEffect, useRef, useState, type MouseEvent, type RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { useHouseholdData } from '../../app/HouseholdData';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { CursorPager } from '../../components/ui/cursor-pager';
import { ListSkeleton } from '../../components/ui/list-skeleton';
import { LoadError } from '../../components/ui/load-error';
import { Notice } from '../../components/ui/notice';
import { useI18n, type Translate } from '../../i18n';
import { signInMethod } from '../admin/adminApi';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import { RemoveAccountDialog } from '../admin/RemoveAccountDialog';
import { RoleChangeDialog } from '../admin/RoleChangeDialog';
import { useAccountRemoval, type RemovalTarget } from '../admin/useAccountRemoval';
import { useRoleChange, type NoticeSpec } from '../admin/useRoleChange';
import { AddAccountDialog } from './AddAccountDialog';
import {
  changeAdminHouseholdAccountRole, readAdminHouseholdAccount, removeAdminHouseholdAccount,
  type AdminHouseholdAccount,
} from './instanceAdminApi';
import { InstallationGone, InstallationOnlyAdmins, BACK_LINK_LOOK } from './InstallationStates';
import { useAccountAssignment, type Assignment } from './useAccountAssignment';
import { useAdminHouseholdAccounts, type InstanceList } from './useInstanceAdminList';

/** Why the viewer's own membership in this household changed, which decides what the shell must assume about it. */
export type SelfChangeKind = 'role' | 'removal' | 'assignment';

/**
 * One household of the installation: who belongs to it, which role they hold and how they sign in, with the three
 * membership actions an installation administrator has here — add an existing account, change a role, or remove
 * someone from this household. Every read and every change uses the installation's own account routes, the viewer's
 * own row included, so this screen never reaches into the household itself and never opens it.
 *
 * A change to the viewer's own membership keeps this screen where it is: demoting themselves, or even removing
 * themselves from this household, costs no access to the Admin center, so it is not treated as leaving. The shell
 * still reads the session, because the server may have moved the viewer's default household — adding the viewer
 * themselves included; only when it did does this screen come back through a remount, and then `handoffNotice` carries
 * the notice and the heading the focus.
 *
 * `name` is only what the list already showed on the way in; a direct load or a reload shows the generic heading
 * instead of reading the list again to find it.
 */
export function InstallationHouseholdScreen({
  householdId, defaultHouseholdId, name, viewerId, login, handoffNotice, onSelfChange, onHandoffDone,
  onSignedOut, onForbidden, onBack,
}: {
  householdId: string;
  /** The household the shell is showing: the only one whose role the navigation rail follows. */
  defaultHouseholdId: string | null;
  name: string | null;
  viewerId: string;
  login: string;
  /**
   * A notice handed back by the shell because an action elsewhere — a self change, or a household created here —
   * remounted or opened this screen; spoken once, then cleared.
   */
  handoffNotice: NoticeSpec | null;
  /** A self change was accepted: the shell reads the session again, so it follows the households really held. */
  onSelfChange: (notice: NoticeSpec, kind: SelfChangeKind) => void;
  onHandoffDone: () => void;
  onSignedOut: () => void;
  onForbidden: () => void;
  onBack: () => void;
}) {
  const { t } = useI18n();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const { notice, announce } = useLiveNotice(handoffNotice, headingRef, onHandoffDone);
  const list = useAdminHouseholdAccounts(householdId, { onSignedOut, onForbidden });
  const { change, removal, busy } = useMembershipActions({
    householdId, defaultHouseholdId, viewerId, list, announce, onSelfChange, onSignedOut, onForbidden,
  });
  const assignment = useAccountAssignment(householdId, {
    viewerId,
    onSignedOut,
    onNotice: announce,
    onReload: list.reload,
    // The viewer added themselves: whether this became their household is the session's answer, not this screen's.
    onSelfAssigned: (next) => { announce(next); onSelfChange(next, 'assignment'); },
    onForbidden: () => { list.markForbidden(); onForbidden(); },
  });

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading headingRef={headingRef} className="text-h1 font-semibold text-ink">
        <span className="break-words">{name ?? t('install.household.title')}</span>
      </Heading>
      {/* The one live host, right under the heading, exactly as the household Admin center keeps it. */}
      <div role="status" aria-atomic="true" className="mt-6 empty:mt-0">
        {notice ? <Notice tone={notice.tone === 'success' ? 'success' : 'neutral'} role={null}>{t(notice.key, notice.params)}</Notice> : null}
      </div>
      <AdminSectionSwitcher current="households" />
      <NavLink href="/admin/households" onNavigate={onBack} {...BACK_LINK_LOOK}>{t('install.household.back')}</NavLink>
      {list.state === 'loading' ? <ListSkeleton label={t('admin.loading')} /> : null}
      {list.state === 'error'
        ? <LoadError className="mt-8" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={list.retry} />
        : null}
      {list.state === 'notFound' ? <InstallationGone t={t} /> : null}
      {list.state === 'forbidden' ? <InstallationOnlyAdmins t={t} /> : null}
      {list.state === 'ready' ? <Accounts
        accounts={list.items}
        viewerId={viewerId}
        stack={list.stack}
        nextCursor={list.nextCursor}
        busy={busy}
        addBusy={change.busy || removal.busy}
        headingRef={list.headingRef}
        onPrevious={list.previous}
        onNext={list.next}
        onAdd={assignment.start}
        onChange={change.request}
        onRemove={removal.request}
        pageFailure={list.pageFailure !== null}
        onRetryPage={list.retryPage}
        t={t}
      /> : null}
    </section>
    <ScreenDialogs viewerId={viewerId} householdName={name} change={change} removal={removal} assignment={assignment} />
  </Layout>;
}

/**
 * The screen's live notice. An action that opened this screen — a self change whose session read moved the default
 * household, or a household just created — remounts or mounts it, so the shell hands the notice back with
 * `handoffNotice`: it is this screen's first notice, and the heading takes the focus the change left on the document.
 * Every other outcome is announced here directly.
 */
function useLiveNotice(handoffNotice: NoticeSpec | null, headingRef: RefObject<HTMLHeadingElement | null>, onDone: () => void) {
  const [notice, setNotice] = useState<NoticeSpec | null>(handoffNotice);
  useEffect(() => {
    if (!handoffNotice) return;
    // The change took the focus with the element it lived on; the heading says where the reader still is.
    const active = document.activeElement;
    if (active === null || active === document.body) headingRef.current?.focus();
    onDone();
  }, [handoffNotice, onDone, headingRef]);
  return { notice, announce: setNotice };
}

/**
 * The two membership actions of this screen, wired to the installation's account routes. An accepted change to the
 * viewer's own membership is announced here: the row shows the server's answer, the notice speaks now, and the shell
 * reads the session again because the default household may have moved. A self role change in the household the shell
 * is showing is followed by a refresh of that household, so the navigation rail shows the role the viewer really
 * holds; that is the provider's read of the default household, never a read this screen makes of the one it shows.
 */
function useMembershipActions({ householdId, defaultHouseholdId, viewerId, list, announce, onSelfChange, onSignedOut, onForbidden }: {
  householdId: string;
  defaultHouseholdId: string | null;
  viewerId: string;
  list: InstanceList<AdminHouseholdAccount>;
  announce: (notice: NoticeSpec) => void;
  onSelfChange: (notice: NoticeSpec, kind: 'role' | 'removal') => void;
  onSignedOut: () => void;
  onForbidden: () => void;
}) {
  const household = useHouseholdData();
  const selfChanged = (notice: NoticeSpec, kind: 'role' | 'removal') => {
    announce(notice);
    onSelfChange(notice, kind);
  };
  // An authoritative `instance_admin_required` says the grant hint is stale: drop the rows and let the shell re-read.
  const denied = () => { list.markForbidden(); onForbidden(); };
  const change = useRoleChange<AdminHouseholdAccount>({
    viewerId,
    onSignedOut,
    onNotice: announce,
    onReload: list.reload,
    onAccount: list.replaceItem,
    onForbidden: denied,
    // Staying here is the point: the installation grant, not this household role, is what opened this screen.
    onSelfChanged: (account, nextRole) => {
      list.replaceItem(account);
      selfChanged({
        key: nextRole === 'admin' ? 'install.notice.selfPromoted' : 'install.notice.selfDemoted',
        tone: 'success',
      }, 'role');
      // Only the household the shell shows carries a role the rail reads, and only a role change leaves the viewer
      // in it: a removal moves the default household, which the shell's own session read already settles.
      if (defaultHouseholdId === householdId) household?.refresh();
    },
    read: (userId) => readAdminHouseholdAccount(householdId, userId),
    change: (userId, role, etag) => changeAdminHouseholdAccountRole(householdId, userId, role, etag),
  });
  const removal = useAccountRemoval({
    viewerId,
    onSignedOut,
    onNotice: announce,
    onReload: list.reload,
    onForbidden: denied,
    // The viewer's own row leaves this household, not the Admin center: the list is read again and this screen stays.
    onSelfLeft: (alreadyGone) => {
      list.reload();
      selfChanged({ key: 'install.notice.selfRemoved', tone: alreadyGone ? 'neutral' : 'success' }, 'removal');
    },
    read: (target) => readAdminHouseholdAccount(householdId, target.userId),
    remove: (userId, etag) => removeAdminHouseholdAccount(householdId, userId, etag),
  });
  return { change, removal, busy: list.busy || change.busy || removal.busy };
}

type ChangeState = Pick<ReturnType<typeof useRoleChange<AdminHouseholdAccount>>, 'pending' | 'busy' | 'triggerRef' | 'confirm' | 'cancel'>;
type RemovalState = Pick<ReturnType<typeof useAccountRemoval>, 'pending' | 'busy' | 'triggerRef' | 'confirm' | 'cancel'>;

/** The dialogs this screen can open; the hooks above own every outcome they can announce. */
function ScreenDialogs({ viewerId, householdName, change, removal, assignment }: {
  viewerId: string;
  householdName: string | null;
  change: ChangeState;
  removal: RemovalState;
  assignment: Assignment;
}) {
  return <>
    <AddAccountDialog assignment={assignment} householdName={householdName} />
    <RoleChangeDialog
      pending={change.pending}
      viewerId={viewerId}
      busy={change.busy}
      triggerRef={change.triggerRef}
      onConfirm={() => { void change.confirm(); }}
      onCancel={change.cancel}
      selfDemoteBody="install.dialog.selfDemote.body"
    />
    {/* The household may not be named on a direct load; the dialog says “this household” instead of hiding the action. */}
    <RemoveAccountDialog
      pending={removal.pending}
      viewerId={viewerId}
      householdName={householdName}
      busy={removal.busy}
      triggerRef={removal.triggerRef}
      onConfirm={() => { void removal.confirm(); }}
      onCancel={removal.cancel}
    />
  </>;
}

/** The member list: the same flat divided rows and pager the household Admin center uses, with its three actions. */
function Accounts({ accounts, viewerId, stack, nextCursor, busy, addBusy, headingRef, onPrevious, onNext, onAdd, onChange, onRemove, pageFailure, onRetryPage, t }: {
  accounts: AdminHouseholdAccount[];
  viewerId: string;
  stack: string[];
  nextCursor: string | null;
  /** A row's action or the list itself is working: the rows wait for it. */
  busy: boolean;
  /**
   * The other two dialogs are working. The Add trigger waits only for them and never for the list's own reload: the
   * add dialog hands the focus back to it on success, and a control that is disabled at that instant drops the
   * reader on the document instead.
   */
  addBusy: boolean;
  headingRef: RefObject<HTMLHeadingElement | null>;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  onAdd: (trigger: HTMLElement) => void;
  onChange: (account: AdminHouseholdAccount, trigger: HTMLElement) => void;
  onRemove: (target: RemovalTarget, trigger: HTMLElement) => void;
  pageFailure: boolean;
  onRetryPage: () => void;
  t: Translate;
}) {
  return <>
    <section aria-labelledby="install-accounts-heading" className="mt-6 sm:mt-8">
      {/* One primary action, on the heading's line; a phone wraps it under the title rather than squeezing both. */}
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h2 id="install-accounts-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
          {t('admin.list.title')}
        </h2>
        <Button disabled={addBusy} onClick={(event) => onAdd(event.currentTarget)}>{t('install.add.action')}</Button>
      </div>
      <p className="mt-1 max-w-prose text-small text-muted">{t('install.household.explained')}</p>
      {accounts.length
        ? <ul aria-labelledby="install-accounts-heading" className="mt-3 divide-y divide-line border-t border-line">
          {accounts.map((account) => <AccountRow
            key={account.userId}
            account={account}
            self={account.userId === viewerId}
            busy={busy}
            onChange={onChange}
            onRemove={onRemove}
            t={t}
          />)}
        </ul>
        : <p className="mt-3 border-t border-line pt-3 text-body text-muted">{t('admin.list.empty')}</p>}
      {stack.length > 0 || nextCursor
        ? <CursorPager kind="account" stack={stack} cursor={nextCursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
        : null}
    </section>
    {pageFailure
      ? <LoadError className="mt-8" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={onRetryPage} />
      : null}
  </>;
}

/**
 * One flat account row, like the household Admin center's: the login (with “(you)” on the viewer's own) over the
 * muted line that states the role and the sign-in method, then the two actions. Removal comes last, and the viewer's
 * own row asks about leaving instead.
 */
function AccountRow({ account, self, busy, onChange, onRemove, t }: {
  account: AdminHouseholdAccount;
  self: boolean;
  busy: boolean;
  onChange: (account: AdminHouseholdAccount, trigger: HTMLElement) => void;
  onRemove: (target: RemovalTarget, trigger: HTMLElement) => void;
  t: Translate;
}) {
  const isAdmin = account.role === 'admin';
  return <li data-install-account-id={account.userId} className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3">
    <div className="min-w-0 flex-1 basis-40">
      <p className="min-w-0 break-words text-row-title font-semibold text-ink">
        <span data-install-login>{account.login}</span>
        {self ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}
      </p>
      <p data-install-meta className="mt-0.5 text-meta leading-snug text-muted">
        <span data-install-role>{t(isAdmin ? 'members.role.admin' : 'members.role.member')}</span>
        {' '}<span aria-hidden="true" className="select-none">·</span>{' '}
        <span>{t(`admin.signIn.${signInMethod(account)}`)}</span>
      </p>
    </div>
    <div className="flex w-full flex-wrap gap-2 sm:w-auto sm:justify-end">
      <Button
        variant="secondary"
        size="small"
        disabled={busy}
        aria-label={t(isAdmin ? 'admin.action.demoteLabel' : 'admin.action.promoteLabel', { login: account.login })}
        onClick={(event) => onChange(account, event.currentTarget)}
      >
        {t(isAdmin ? 'admin.action.demote' : 'admin.action.promote')}
      </Button>
      <Button
        variant="secondary"
        size="small"
        disabled={busy}
        aria-label={t(self ? 'admin.action.leaveLabel' : 'admin.action.removeLabel', { login: account.login })}
        onClick={(event) => onRemove({ userId: account.userId, login: account.login, role: account.role }, event.currentTarget)}
      >
        {t(self ? 'admin.action.leave' : 'admin.action.remove')}
      </Button>
    </div>
  </li>;
}
