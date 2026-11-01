import { useState, useSyncExternalStore } from 'react';
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
  route: Route; session: Session; inviteVisit: string | null; inviteToken: string | null;
  allItemsVisit: number; transitioned: { current: boolean }; oidcOutcome: OidcOutcome | null;
  navigation: ReturnType<typeof useAppNavigation>; sessionActions: ReturnType<typeof useAppSession>;
};

export function SignedInApp(props: Props) {
  const session = props.session;
  const { route, navigation } = props;
  const householdId = session.defaultHouseholdId;
  const navRoute = householdNavRoute(route, Boolean(householdId));
  const actions = navigationActions(navigation);
  const desktopPane = useDesktopItemPane(route.kind === 'item' && Boolean(householdId));
  if (route.kind === 'invite') return renderInvite(props);
  if (desktopPane && route.kind === 'item' && householdId) return <DesktopItemPane props={props} itemId={route.itemId} householdId={householdId} navRoute={navRoute} actions={actions} />;
  return <HouseholdRouteView props={props} navRoute={navRoute} actions={actions} />;
}

function navigationActions(navigation: Props['navigation']): NavigationActions {
  return { home: navigation.goHome, allItems: navigation.openAllItems, people: navigation.openPeople, members: navigation.openMembers, account: navigation.openAccount };
}

function renderInvite(props: Props) {
  return <ScreenTransitionContext value={props.transitioned}><InviteScreen key={props.inviteVisit} token={props.inviteToken} session={props.session} onSignedIn={props.sessionActions.signedIn} onSignedOut={props.sessionActions.inviteSignedOut} onAccountReady={props.sessionActions.accountReady} onJoinedSignedOut={props.sessionActions.sessionEnded} /></ScreenTransitionContext>;
}

function HouseholdRouteView({ props, navRoute, actions }: { props: Props; navRoute: ReturnType<typeof householdNavRoute>; actions: NavigationActions }) {
  const { route, session, navigation, sessionActions } = props;
  const householdId = session.defaultHouseholdId;
  const oidcOutcome = props.oidcOutcome;
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdNavigationContext value={{ route: navRoute, actions, household: Boolean(householdId) }}>
      <ScreenTransitionContext value={props.transitioned}>
        {route.kind === 'account' ? <AccountScreen login={session.login} outcome={oidcOutcome} onSignedOut={sessionActions.sessionEnded} />
          : route.kind === 'people' && householdId ? <SubjectsScreen userId={session.userId} householdId={householdId} login={session.login} onSignedOut={sessionActions.sessionEnded} />
            : route.kind === 'members' && householdId ? <MembersScreen householdId={householdId} userId={session.userId} login={session.login} onSignedOut={sessionActions.sessionEnded} />
              : route.kind === 'allItems' && householdId ? <AllItemsScreen key={props.allItemsVisit} householdId={householdId} login={session.login} userId={session.userId} onBack={navigation.goHome} onSignedOut={sessionActions.sessionEnded} onOpenItem={navigation.openItemFromAllItems} />
                : route.kind === 'item' && householdId ? <ItemDetailScreen key={`${householdId}:${route.itemId}`} householdId={householdId} itemId={route.itemId} login={session.login} onBack={navigation.goBackFromItem} onSignedOut={sessionActions.sessionEnded} />
                  : route.kind === 'invalidItem' && householdId ? <ItemDetailScreen key={`${householdId}:invalid`} householdId={householdId} itemId="invalid" login={session.login} onBack={navigation.goHome} onSignedOut={sessionActions.sessionEnded} />
                    : <HomeScreen session={session} onSignedOut={sessionActions.sessionEnded} onOpenPeople={navigation.openPeople} onOpenAllItems={navigation.openAllItemsForStatus} onOpenItem={navigation.openItemFromHome} />}
      </ScreenTransitionContext>
    </HouseholdNavigationContext>
  </SignOutContext>;
}

const desktopMedia = typeof window.matchMedia === 'function' ? window.matchMedia('(min-width: 1024px)') : null;

function useDesktopItemPane(selected: boolean) {
  const matches = useSyncExternalStore(
    (notify) => {
      if (!desktopMedia) return () => undefined;
      desktopMedia.addEventListener('change', notify);
      return () => desktopMedia.removeEventListener('change', notify);
    },
    () => desktopMedia?.matches ?? false,
    () => false,
  );
  return selected && matches;
}

function DesktopItemPane({ props, itemId, householdId, navRoute, actions }: {
  props: Props; itemId: string; householdId: string; navRoute: ReturnType<typeof householdNavRoute>; actions: NavigationActions;
}) {
  const { session, navigation, sessionActions } = props;
  const [homeRevision, setHomeRevision] = useState(0);
  return <SignOutContext value={sessionActions.signedOut}>
    <HouseholdNavigationContext value={{ route: navRoute, actions, household: true }}>
      <ScreenTransitionContext value={props.transitioned}>
        <div className="desktop-item-shell">
          <div className="desktop-item-list">
            <div className="desktop-item-brand">Tendo</div>
            <HomeScreen key={homeRevision} session={session} onSignedOut={sessionActions.sessionEnded} onOpenPeople={navigation.openPeople} onOpenAllItems={navigation.openAllItemsForStatus} onOpenItem={navigation.openItemFromHome} />
          </div>
          <ItemDetailScreen key={`${householdId}:${itemId}`} embedded householdId={householdId} itemId={itemId} login={session.login} onBack={navigation.goBackFromItem} onSignedOut={sessionActions.sessionEnded} onChanged={() => setHomeRevision((revision) => revision + 1)} />
        </div>
      </ScreenTransitionContext>
    </HouseholdNavigationContext>
  </SignOutContext>;
}
