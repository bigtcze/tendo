import { AccountScreen } from '../features/auth/AccountScreen';
import type { MessageKey } from '../i18n';
import type { useAppNavigation } from './useAppNavigation';
import type { useAppSession } from './useAppSession';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { AllItemsScreen } from '../features/items/AllItemsScreen';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { MembersScreen } from '../features/members/MembersScreen';
import { InviteScreen } from '../features/members/InviteScreen';
import { AdminScreen } from '../features/admin/AdminScreen';
import { ScreenTransitionContext } from './Heading';
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
  homeNotice: MessageKey | null;
  onDismissNotice: () => void;
  onContinuePending: () => void;
  navigation: ReturnType<typeof useAppNavigation>;
  sessionActions: ReturnType<typeof useAppSession>;
};

export function SignedInApp(props: Props) {
  const { route, session, navigation } = props;
  const householdId = session.defaultHouseholdId;
  const navRoute = householdNavRoute(route, Boolean(householdId));
  const navigationActions = {
    home: navigation.goHome,
    allItems: navigation.openAllItems,
    search: navigation.searchAllItems,
    people: navigation.openPeople,
    members: navigation.openMembers,
    account: navigation.openAccount,
    admin: navigation.openAdmin,
  };
  if (route.kind === 'invite') return renderInvite(props);
  if (route.kind === 'notFound') return renderNotFound(props, householdId ?? null, navigationActions);
  return renderHouseholdScreens(props, householdId ?? null, navRoute, navigationActions);
}

/** Unknown pages keep the signed-in shell (account actions and navigation); Home is the nearest destination. */
function renderNotFound(props: Props, householdId: string | null, navigationActions: NavigationActions) {
  const { session, sessionActions } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{ route: 'home', actions: navigationActions, household: Boolean(householdId) }}>
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
function renderRouteScreen(props: Props, householdId: string | null) {
  const { route, session, navigation, sessionActions } = props;
  if (route.kind === 'account') {
    return <AccountScreen login={session.login} onSignedOut={sessionActions.sessionEnded} />;
  }
  if (route.kind === 'admin' && householdId) {
    return <AdminScreen householdId={householdId} viewerId={session.userId} login={session.login} onSignedOut={sessionActions.sessionEnded} />;
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

function renderHouseholdScreens(
  props: Props,
  householdId: string | null,
  navRoute: ReturnType<typeof householdNavRoute>,
  navigationActions: NavigationActions,
) {
  const { sessionActions } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdDataProvider householdId={householdId} onSignedOut={sessionActions.sessionEnded}>
      <HouseholdNavigationContext value={{ route: navRoute, actions: navigationActions, household: Boolean(householdId) }}>
        <ScreenTransitionContext value={props.transitioned}>{renderRouteScreen(props, householdId)}</ScreenTransitionContext>
      </HouseholdNavigationContext>
    </HouseholdDataProvider>
  </SignOutContext>;
}
