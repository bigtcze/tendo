import { InviteScreen } from '../features/members/InviteScreen';
import { ConnectRoute } from './ConnectRoute';
import { ResetRoute } from './ResetRoute';
import { useConnectVisit } from './useConnectVisit';
import { useHomeNotice } from './useHomeNotice';
import { ScreenTransitionContext } from './Heading';
import { SignedOutApp } from './SignedOutApp';
import { SignedInApp } from './SignedInApp';
import { useAppRoute } from './useAppRoute';
import { useAppNavigation } from './useAppNavigation';
import { useAppSession } from './useAppSession';
import { useInviteVisit } from './useInviteVisit';
import { usePasswordResetVisit } from './usePasswordResetVisit';
import { useOidcEntry } from './useOidcEntry';
import { useOidcOutcome } from './useOidcOutcome';

export function App() {
  const [oidcOutcome, setOidcOutcome] = useOidcOutcome();
  const entry = useOidcEntry();
  const invite = useInviteVisit();
  const resetToken = usePasswordResetVisit();
  const connect = useConnectVisit();
  const routeState = useAppRoute();
  const session = useAppSession({
    setRoute: routeState.setRoute,
    transitionedRef: routeState.transitioned,
    setOidcOutcome,
    entry,
  });
  const [homeNotice, dismissHomeNotice] = useHomeNotice(session, entry);
  const navigation = useAppNavigation({
    route: routeState.route,
    setRoute: routeState.setRoute,
    transitionedRef: routeState.transitioned,
    setAllItemsVisit: routeState.setAllItemsVisit,
    setOidcOutcome,
    clearHomeNotice: dismissHomeNotice,
    state: session.state,
  });
  const { state } = session;

  if (routeState.route.kind === 'resetPassword') {
    return <ResetRoute token={resetToken} signedIn={state.kind === 'signedIn'} transitioned={routeState.transitioned} onRedeemed={session.passwordResetCompleted} />;
  }
  if (state.kind === 'signedIn' && routeState.route.kind === 'connect') {
    return <ConnectRoute visit={connect} session={state.session} transitioned={routeState.transitioned} onSignedOut={session.connectSignedOut} onHome={navigation.goHome} />;
  }
  if (state.kind === 'signedIn') {
    return (
      <SignedInApp
        route={routeState.route}
        session={state.session}
        inviteVisit={invite.inviteVisit}
        inviteToken={invite.inviteToken}
        allItemsVisit={routeState.allItemsVisit}
        transitioned={routeState.transitioned}
        homeNotice={homeNotice}
        onDismissNotice={dismissHomeNotice}
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
          passwordResetNotice={session.passwordResetNotice}
          loginPrefill={session.loginPrefill}
          oidcContinue={entry.continueRequested}
          oidcContinueExpired={session.oidcContinueExpired}
          continueCancelled={session.continueCancelled}
          continueExpired={session.continueExpired}
          connectOutcome={connect.outcome}
        />
      )}
    </ScreenTransitionContext>
  );
}
