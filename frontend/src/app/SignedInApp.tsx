import { AccountScreen } from '../features/auth/AccountScreen';
import { useState } from 'react';
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
  const householdId = session.defaultHouseholdId;
  const navRoute = householdNavRoute(route, Boolean(householdId));
  const navigationActions: NavigationActions = {
    home: navigation.goHome,
    allItems: navigation.openAllItems,
    archive: navigation.openArchive,
    people: navigation.openPeople,
    members: navigation.openMembers,
    account: navigation.openAccount,
    admin: navigation.openAdmin,
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
        : renderHouseholdScreens(props, householdId ?? null, navRoute, navigationActions, selfLeft)}
    </HouseholdSwitchProvider>
    {/* WCAG 4.1.3: one polite host sits beside the screens and never remounts, so a switch is spoken once Home has
        taken over. It carries no status role, so it never competes with the screen's own mutation notice. */}
    <p aria-live="polite" aria-atomic="true" className="sr-only">{announcement}</p>
  </>;
}

/** Unknown pages keep the signed-in shell (account actions and navigation); Home is the nearest destination. */
function renderNotFound(props: Props, householdId: string | null, navigationActions: NavigationActions) {
  const { session, sessionActions, navigation } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider key={householdId ?? 'none'} householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{
        route: 'home', actions: navigationActions, household: Boolean(householdId),
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
function renderRouteScreen(props: Props, householdId: string | null, onSelfLeft: (household: string, alreadyGone: boolean) => void) {
  const { route, session, navigation, sessionActions } = props;
  if (route.kind === 'account') {
    return <AccountScreen
      session={session}
      onSessionChanged={sessionActions.updateSession}
      onSignedOut={sessionActions.sessionEnded}
      onSelfLeft={onSelfLeft}
    />;
  }
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
 * The household subtree (and its data) is keyed by the household it shows: after a switch it mounts again with the
 * new household's own requests, and nothing keeps reading the household that was left behind.
 */
function renderHouseholdScreens(
  props: Props,
  householdId: string | null,
  navRoute: ReturnType<typeof householdNavRoute>,
  navigationActions: NavigationActions,
  onSelfLeft: (household: string, alreadyGone: boolean) => void,
) {
  const { sessionActions, navigation } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider key={householdId ?? 'none'} householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{
        route: navRoute, actions: navigationActions, household: Boolean(householdId),
        archivedView: navigation.archivedView, itemOrigin: navigation.itemOrigin,
      }}>
        <ScreenTransitionContext value={props.transitioned}>{renderRouteScreen(props, householdId, onSelfLeft)}</ScreenTransitionContext>
      </HouseholdNavigationContext>
    </HouseholdDataProvider>
  </SignOutContext>;
}
