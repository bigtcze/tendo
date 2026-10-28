import { AccountScreen } from '../features/auth/AccountScreen';
import type { OidcOutcome } from '../features/auth/oidcApi';
import type { useAppNavigation } from './useAppNavigation';
import type { useAppSession } from './useAppSession';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { AllItemsScreen } from '../features/items/AllItemsScreen';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { MembersScreen } from '../features/members/MembersScreen';
import { InviteScreen } from '../features/members/InviteScreen';
import { ScreenTransitionContext } from './Heading';
import { SignOutContext } from './SignOutContext';
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
  oidcOutcome: OidcOutcome | null;
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
    people: navigation.openPeople,
    members: navigation.openMembers,
    account: navigation.openAccount,
  };
  if (route.kind === 'invite') return renderInvite(props);
  return renderHouseholdScreens(props, householdId ?? null, navRoute, navigationActions);
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

function renderHouseholdScreens(
  props: Props,
  householdId: string | null,
  navRoute: ReturnType<typeof householdNavRoute>,
  navigationActions: NavigationActions,
) {
  const { route, session, navigation, sessionActions } = props;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdNavigationContext value={{ route: navRoute, actions: navigationActions, household: Boolean(householdId) }}>
      <ScreenTransitionContext value={props.transitioned}>
        {route.kind === 'account' ? (
          <AccountScreen
            login={session.login}
            outcome={props.oidcOutcome}
            onSignedOut={sessionActions.sessionEnded}
          />
        ) : route.kind === 'people' && householdId ? (
          <SubjectsScreen
            userId={session.userId}
            householdId={householdId}
            login={session.login}
            onSignedOut={sessionActions.sessionEnded}
          />
        ) : route.kind === 'members' && householdId ? (
          <MembersScreen
            householdId={householdId}
            userId={session.userId}
            login={session.login}
            onSignedOut={sessionActions.sessionEnded}
          />
        ) : route.kind === 'allItems' && householdId ? (
          <AllItemsScreen
            key={props.allItemsVisit}
            householdId={householdId}
            login={session.login}
            userId={session.userId}
            onBack={navigation.goHome}
            onSignedOut={sessionActions.sessionEnded}
            onOpenItem={navigation.openItemFromAllItems}
          />
        ) : route.kind === 'item' && householdId ? (
          <ItemDetailScreen
            key={`${householdId}:${route.itemId}`}
            householdId={householdId}
            itemId={route.itemId}
            login={session.login}
            onBack={navigation.goBackFromItem}
            onSignedOut={sessionActions.sessionEnded}
          />
        ) : route.kind === 'invalidItem' && householdId ? (
          <ItemDetailScreen
            key={`${householdId}:invalid`}
            householdId={householdId}
            itemId="invalid"
            login={session.login}
            onBack={navigation.goHome}
            onSignedOut={sessionActions.sessionEnded}
          />
        ) : (
          <HomeScreen
            session={session}
            onSignedOut={sessionActions.sessionEnded}
            onOpenPeople={navigation.openPeople}
            onOpenAllItems={navigation.openAllItemsForStatus}
            onOpenItem={navigation.openItemFromHome}
          />
        )}
      </ScreenTransitionContext>
    </HouseholdNavigationContext>
  </SignOutContext>;
}
