import { InviteScreen } from '../features/members/InviteScreen';
import { ScreenTransitionContext } from './Heading';
import { SignedOutApp } from './SignedOutApp';
import { SignedInApp } from './SignedInApp';
import { useAppRoute } from './useAppRoute';
import { useAppNavigation } from './useAppNavigation';
import { useAppSession } from './useAppSession';
import { useInviteVisit } from './useInviteVisit';
import { useOidcEntry } from './useOidcEntry';
import { useOidcOutcome } from './useOidcOutcome';

export function App() {
  const [oidcOutcome, setOidcOutcome] = useOidcOutcome();
  const entry = useOidcEntry();
  const invite = useInviteVisit();
  const routeState = useAppRoute();
  const session = useAppSession({
    setRoute: routeState.setRoute,
    transitionedRef: routeState.transitioned,
    setOidcOutcome,
    entry,
  });
  const navigation = useAppNavigation({
    route: routeState.route,
    setRoute: routeState.setRoute,
    transitionedRef: routeState.transitioned,
    setAllItemsVisit: routeState.setAllItemsVisit,
    setOidcOutcome,
    clearHomeNotice: entry.clearNotice,
    state: session.state,
  });
  const state = session.state;

  if (state.kind === 'signedIn') {
    return (
      <SignedInApp
        route={routeState.route}
        session={state.session}
        inviteVisit={invite.inviteVisit}
        inviteToken={invite.inviteToken}
        allItemsVisit={routeState.allItemsVisit}
        transitioned={routeState.transitioned}
        oidcOutcome={oidcOutcome}
        homeNotice={entry.notice}
        onDismissNotice={entry.clearNotice}
        onContinuePending={session.pendingSignedOut}
        navigation={navigation}
        sessionActions={session}
      />
    );
  }

  return (
    <ScreenTransitionContext value={routeState.transitioned}>
      {state.kind === 'signedOut' && routeState.route.kind === 'invite' ? (
        <InviteScreen
          key={invite.inviteVisit}
          token={invite.inviteToken}
          onSignedIn={session.signedIn}
          onSignedOut={session.inviteSignedOut}
          onAccountReady={session.accountReady}
          onJoinedSignedOut={session.signedOut}
          focusLogin={session.inviteFocusLogin}
        />
      ) : (
        <SignedOutApp
          state={state}
          retry={session.retry}
          signedIn={session.signedIn}
          signedOut={session.signedOut}
          accountReadyNotice={session.accountReadyNotice}
          oidcOutcome={oidcOutcome}
          sessionEndedNotice={session.sessionEndedNotice}
          oidcContinue={entry.continueRequested}
          oidcContinueExpired={session.oidcContinueExpired}
          continueCancelled={session.continueCancelled}
          continueExpired={session.continueExpired}
        />
      )}
    </ScreenTransitionContext>
  );
}
