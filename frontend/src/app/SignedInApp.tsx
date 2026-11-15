import { AccountScreen } from '../features/auth/AccountScreen';
import { useCallback, useState } from 'react';
import { useI18n } from '../i18n';
import { fetchSession } from '../features/auth/sessionApi';
import type { useAppNavigation } from './useAppNavigation';
import type { useAppSession } from './useAppSession';
import type { NoticeMessage } from './notices';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { AllItemsScreen } from '../features/items/AllItemsScreen';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { MembersScreen } from '../features/members/MembersScreen';
import { InviteScreen } from '../features/members/InviteScreen';
import { AdminScreen } from '../features/admin/AdminScreen';
import { InstallationHouseholdsScreen } from '../features/installation/InstallationHouseholdsScreen';
import { InstallationHouseholdScreen, type SelfChangeKind } from '../features/installation/InstallationHouseholdScreen';
import type { AdminHousehold } from '../features/installation/instanceAdminApi';
import { rereadInstanceAdminSession } from '../features/installation/installationSession';
import { ScreenTransitionContext } from './Heading';
import { HouseholdSwitchProvider } from './HouseholdSwitchProvider';
import { Layout } from './Layout';
import { NotFoundScreen } from './StatusScreens';
import { SignOutContext } from './SignOutContext';
import { AccountActions } from './AccountActions';
import { HouseholdDataProvider } from './HouseholdData';
import { HouseholdNavigationContext, type NavigationActions } from './HouseholdNavigation';
import { householdNavRoute, type Route } from './routes';
import type { Session } from '../features/auth/sessionApi';
import type { NoticeSpec } from '../features/admin/useRoleChange';

type Props = {
  route: Route;
  session: Session;
  inviteVisit: string | null;
  inviteToken: string | null;
  allItemsVisit: number;
  transitioned: { current: boolean };
  homeNotice: NoticeMessage | null;
  onDismissNotice: () => void;
  onContinuePending: () => void;
  navigation: ReturnType<typeof useAppNavigation>;
  sessionActions: ReturnType<typeof useAppSession>;
};

export function SignedInApp(props: Props) {
  const { route, session, navigation, sessionActions } = props;
  const { t } = useI18n();
  const [announcement, setAnnouncement] = useState('');
  // The installation Admin center's notice about the viewer's own membership, or about a household just created. It
  // lives here, above the household keys, because a re-read session can move the default household and remount the
  // screen that reported it.
  const [installHandoff, setInstallHandoff] = useState<InstallHandoff | null>(null);
  const clearInstallHandoff = useCallback(() => setInstallHandoff(null), []);
  const householdId = session.defaultHouseholdId;
  // A handoff belongs to the visit that reported it: leaving that visit — another route, or another household —
  // ends it, so a notice can never surface on a later one.
  const visitedHousehold = route.kind === 'adminHousehold' ? route.householdId : null;
  if (installHandoff !== null && installHandoff.householdId !== visitedHousehold) setInstallHandoff(null);
  const navRoute = householdNavRoute(route, Boolean(householdId));
  const navigationActions: NavigationActions = {
    home: navigation.goHome,
    allItems: navigation.openAllItems,
    archive: navigation.openArchive,
    people: navigation.openPeople,
    members: navigation.openMembers,
    account: navigation.openAccount,
    admin: navigation.openAdmin,
    adminHouseholds: navigation.openAdminHouseholds,
  };
  // Adopting a household chosen in the top bar is the same transition as signing in: Home at '/', a new session
  // generation so callbacks from the previous household are ignored, and the returned session as the live one.
  const adopt = (next: Session, name: string) => {
    setAnnouncement(t('household.menu.switched', { name }));
    sessionActions.signedIn(next);
  };
  /**
   * The viewer's own membership is gone. The server has already moved their default membership, so a fresh session
   * read — not the one this screen booted with — decides where Home opens next. When even that read fails, re-boot
   * from the server instead of trusting a household that may no longer be theirs.
   */
  const selfLeft = async (household: string, alreadyGone: boolean) => {
    const result = await fetchSession();
    if (result.kind === 'signedIn') { sessionActions.householdLeft(result.session, household, alreadyGone); return; }
    if (result.kind === 'signedOut') { sessionActions.sessionEnded(); return; }
    sessionActions.retry();
  };
  if (route.kind === 'invite') return renderInvite(props);
  // The bar's household requests live here, above every screen, so a switch is still adopted when the menu that
  // asked for it was replaced on the way. The key restarts them with the new session: the list read for the account
  // that was left behind cannot land in the one that replaced it.
  return <>
    <HouseholdSwitchProvider
      key={`${session.userId}:${householdId ?? 'none'}`}
      onAdopt={adopt}
      onSignedOut={sessionActions.sessionEnded}
    >
      {route.kind === 'notFound'
        ? renderNotFound(props, householdId ?? null, navigationActions)
        : renderHouseholdScreens(props, householdId ?? null, navRoute, navigationActions, selfLeft, {
          handoff: installHandoff, setHandoff: setInstallHandoff, clearHandoff: clearInstallHandoff,
        })}
    </HouseholdSwitchProvider>
    {/* WCAG 4.1.3: one polite host sits beside the screens and never remounts, so a switch is spoken once Home has
        taken over. It carries no status role, so it never competes with the screen's own mutation notice. */}
    <p aria-live="polite" aria-atomic="true" className="sr-only">{announcement}</p>
  </>;
}

type InstallHandoff = { householdId: string; notice: NoticeSpec };
type InstallHandoffState = {
  handoff: InstallHandoff | null;
  setHandoff: (next: InstallHandoff | null) => void;
  /** Stable for the screen's hand-off effect, which calls it once per noticed visit. */
  clearHandoff: () => void;
};

/** Unknown pages keep the signed-in shell (account actions and navigation); Home is the nearest destination. */
function renderNotFound(props: Props, householdId: string | null, navigationActions: NavigationActions) {
  const { session, sessionActions, navigation } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider key={householdId ?? 'none'} householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{
        route: 'home', actions: navigationActions, household: Boolean(householdId), instanceAdmin: session.instanceAdmin,
        archivedView: navigation.archivedView, itemOrigin: navigation.itemOrigin,
      }}>
        <ScreenTransitionContext value={props.transitioned}>
          <Layout actions={<AccountActions login={session.login} onSignedOut={sessionActions.sessionEnded} />}>
            <NotFoundScreen />
          </Layout>
        </ScreenTransitionContext>
      </HouseholdNavigationContext>
    </HouseholdDataProvider>
  </SignOutContext>;
}

function renderInvite(props: Props) {
  const actions = props.sessionActions;
  return <ScreenTransitionContext value={props.transitioned}>
    <InviteScreen
      key={props.inviteVisit}
      token={props.inviteToken}
      session={props.session}
      onSignedIn={actions.signedIn}
      onSignedOut={actions.inviteSignedOut}
      onAccountReady={actions.accountReady}
      onJoinedSignedOut={actions.sessionEnded}
    />
  </ScreenTransitionContext>;
}

/** People, things, items and their details: the household records that need a household to exist. */
function renderHouseholdRecords(props: Props, householdId: string) {
  const { route, session, navigation, sessionActions } = props;
  if (route.kind === 'people') {
    return <SubjectsScreen userId={session.userId} householdId={householdId} login={session.login} onSignedOut={sessionActions.sessionEnded} />;
  }
  if (route.kind === 'members') {
    return <MembersScreen householdId={householdId} userId={session.userId} login={session.login} onSignedOut={sessionActions.sessionEnded} />;
  }
  if (route.kind === 'allItems') {
    return <AllItemsScreen
      key={props.allItemsVisit}
      householdId={householdId}
      login={session.login}
      userId={session.userId}
      onBack={navigation.goHome}
      onSignedOut={sessionActions.sessionEnded}
      onOpenItem={navigation.openItemFromAllItems}
      onArchivedViewChange={navigation.reportItemsView}
    />;
  }
  if (route.kind === 'item' || route.kind === 'invalidItem') {
    const itemId = route.kind === 'item' ? route.itemId : 'invalid';
    return <ItemDetailScreen
      key={`${householdId}:${itemId}`}
      householdId={householdId}
      itemId={itemId}
      login={session.login}
      userId={session.userId}
      onBack={route.kind === 'item' ? navigation.goBackFromItem : navigation.goHome}
      onSignedOut={sessionActions.sessionEnded}
    />;
  }
  return null;
}

/** The destination screen for the current route; every household route falls back to Home without a household. */
function renderRouteScreen(
  props: Props,
  householdId: string | null,
  onSelfLeft: (household: string, alreadyGone: boolean) => void,
  install: InstallHandoffState,
) {
  const { route, session, navigation, sessionActions } = props;
  if (route.kind === 'account') {
    return <AccountScreen
      session={session}
      onSessionChanged={sessionActions.updateSession}
      onSignedOut={sessionActions.sessionEnded}
      onSelfLeft={onSelfLeft}
    />;
  }
  const installation = renderInstallation(props, install);
  if (installation) return installation;
  if (route.kind === 'admin' && householdId) {
    return <AdminScreen householdId={householdId} viewerId={session.userId} login={session.login}
      onSignedOut={sessionActions.sessionEnded} onSelfLeft={onSelfLeft} />;
  }
  const records = householdId ? renderHouseholdRecords(props, householdId) : null;
  if (records) return records;
  return <HomeScreen
    session={session}
    onSignedOut={sessionActions.sessionEnded}
    onOpenPeople={navigation.openPeople}
    onOpenAllItems={navigation.openAllItemsForStatus}
    onOpenItem={navigation.openItemFromHome}
    homeNotice={props.homeNotice}
    onDismissNotice={props.onDismissNotice}
    onContinuePending={props.onContinuePending}
  />;
}

/**
 * The installation-wide side of the Admin center. It belongs to the account, not to a household, so it renders
 * without one; the household list is also what a household-less /admin really means on its first render, before the
 * canonical correction replaces the route with the list's own.
 */
function renderInstallation(props: Props, install: InstallHandoffState) {
  const { route, session, navigation, sessionActions } = props;
  const shared = {
    login: session.login,
    onSignedOut: sessionActions.sessionEnded,
    // A refused installation read means the grant hint is stale; the re-read follows whatever the account really is.
    onForbidden: () => {
      void rereadInstanceAdminSession({
        viewerId: session.userId,
        onSession: sessionActions.updateSession,
        onSignedOut: sessionActions.sessionEnded,
        onReboot: sessionActions.retry,
      });
    },
  };
  /**
   * A household was just created, with the viewer themselves or someone else as its first administrator. The reader
   * lands on the new household, which carries the notice like any page the shell opens on their behalf.
   *
   * A first administrator who is the viewer may also have just been given their first household, so the session is
   * read again before moving: only a fresh session can say where Home opens now, and setting the notice in the same
   * step as the adopted session keeps it with the page even when that read remounts the subtree. Someone else's
   * membership moves nothing about the session, so that path goes straight to the new household.
   */
  const onCreated = (created: AdminHousehold, asSelf: boolean) => {
    const notice: NoticeSpec = { key: 'install.notice.created', params: { household: created.name }, tone: 'success' };
    if (!asSelf) {
      install.setHandoff({ householdId: created.id, notice });
      navigation.openAdminHousehold(created.id, created.name);
      return;
    }
    void rereadInstanceAdminSession({
      viewerId: session.userId,
      onSession: (next) => {
        install.setHandoff({ householdId: created.id, notice });
        sessionActions.updateSession(next);
        navigation.openAdminHousehold(created.id, created.name);
      },
      onSignedOut: sessionActions.sessionEnded,
      // A create can hand a household-less shell its first household, so a read that failed is not trusted.
      onError: sessionActions.retry,
      onReboot: sessionActions.retry,
    });
  };
  const householdList = () => <InstallationHouseholdsScreen
    {...shared}
    viewerId={session.userId}
    onOpenHousehold={navigation.openAdminHousehold}
    onCreated={onCreated}
  />;
  if (route.kind === 'adminHouseholds') return householdList();
  if (route.kind === 'admin') {
    return session.instanceAdmin && !session.defaultHouseholdId ? householdList() : null;
  }
  if (route.kind !== 'adminHousehold') return null;
  // The name is only ever the one this visit carried in; it is dropped when it belongs to another account or page.
  const remembered = navigation.adminHouseholdName;
  const name = remembered?.userId === session.userId && remembered.householdId === route.householdId ? remembered.name : null;
  const target = route.householdId;
  /**
   * The viewer's own membership in this household changed. The screen stays where it is — it never opens the
   * household or treats this as leaving it — but the server may have moved the default household, so the session is
   * read again for the same account.
   *
   * The notice only travels through here when that read really moved the default, because only then does the
   * household subtree remount and take the notice the screen was showing with it. It is set in the same callback as
   * the adopted session, so the remount and the notice happen together; and because the visit is still the current
   * one at that moment, a notice can never outlive the household page that reported it.
   */
  const onSelfChange = (notice: NoticeSpec, kind: SelfChangeKind) => {
    void rereadInstanceAdminSession({
      viewerId: session.userId,
      onSession: (next) => {
        const moved = next.defaultHouseholdId !== session.defaultHouseholdId;
        if (moved) install.setHandoff({ householdId: target, notice });
        sessionActions.updateSession(next);
      },
      onSignedOut: sessionActions.sessionEnded,
      // An assignment never takes a household away from the viewer (ADR 0020), so a failed read changes nothing about
      // a membership they already had. It may, however, have given a household-less shell its first household, and a
      // shell that keeps trusting "no household" is worse than a re-boot from the server. A role change can move
      // nothing at all, so only a failure after an assignment or a removal re-boots.
      onError: kind === 'role' ? undefined : sessionActions.retry,
      onReboot: sessionActions.retry,
    });
  };
  const handoff = install.handoff?.householdId === target ? install.handoff.notice : null;
  return <InstallationHouseholdScreen
    key={target}
    householdId={target}
    defaultHouseholdId={session.defaultHouseholdId ?? null}
    name={name}
    viewerId={session.userId}
    {...shared}
    handoffNotice={handoff}
    onSelfChange={onSelfChange}
    onHandoffDone={install.clearHandoff}
    onBack={navigation.openAdminHouseholds}
  />;
}

/**
 * The household subtree (and its data) is keyed by the household it shows: after a switch it mounts again with the
 * new household's own requests, and nothing keeps reading the household that was left behind.
 */
function renderHouseholdScreens(
  props: Props,
  householdId: string | null,
  navRoute: ReturnType<typeof householdNavRoute>,
  navigationActions: NavigationActions,
  onSelfLeft: (household: string, alreadyGone: boolean) => void,
  install: InstallHandoffState,
) {
  const { sessionActions, navigation } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider key={householdId ?? 'none'} householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{
        route: navRoute, actions: navigationActions, household: Boolean(householdId), instanceAdmin: props.session.instanceAdmin,
        archivedView: navigation.archivedView, itemOrigin: navigation.itemOrigin,
      }}>
        <ScreenTransitionContext value={props.transitioned}>{renderRouteScreen(props, householdId, onSelfLeft, install)}</ScreenTransitionContext>
      </HouseholdNavigationContext>
    </HouseholdDataProvider>
  </SignOutContext>;
}
