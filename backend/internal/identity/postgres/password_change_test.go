package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests pin the persisted effect of a local password replacement. Every
// flow (self-service change, admin reset redemption, operator CLI) shares the
// credentialTx write, so the assertions cover "the whole account state moved",
// not just the hash.

func sessionTokenLike(seed byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

func newSessionService(t *testing.T, app *pgxpool.Pool, clock *passwordResetTestClock) *identity.SessionService {
	t.Helper()
	sessions, err := identity.NewSessionService(New(app, nil), clock.now)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPasswordVerifier(fakeCredentialVerifier)
	return sessions
}

func newPasswordChangeService(t *testing.T, app *pgxpool.Pool, clock *passwordResetTestClock) *identity.PasswordChangeService {
	t.Helper()
	service, err := identity.NewPasswordChangeService(NewPasswordChangeRepository(app), clock.now)
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(password string) (string, error) { return "hash:" + password, nil })
	service.SetPasswordVerifier(fakeCredentialVerifier)
	return service
}

func newAdminPasswordService(t *testing.T, app *pgxpool.Pool, clock *passwordResetTestClock) *identity.AdminPasswordService {
	t.Helper()
	service, err := identity.NewAdminPasswordService(NewPasswordChangeRepository(app), clock.now)
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(password string) (string, error) { return "hash:" + password, nil })
	return service
}

func localCredentialState(t *testing.T, ctx context.Context, admin *pgxpool.Pool, userID string) (string, int64) {
	t.Helper()
	var hash string
	var version int64
	if err := admin.QueryRow(ctx, `SELECT password_hash, credential_version FROM local_credentials WHERE user_id=$1::uuid`, userID).Scan(&hash, &version); err != nil {
		t.Fatal(err)
	}
	return hash, version
}

func insertAccount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, login string) string {
	t.Helper()
	var id string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestPasswordChangePostgresRotatesCallerAndSignsOutEverywhere is the reference
// outcome of a self-service change: the caller keeps exactly one new session,
// every other session dies, the stored hash and version move, and an
// admin-issued reset link from before the change can no longer be redeemed.
func TestPasswordChangePostgresRotatesCallerAndSignsOutEverywhere(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	change := newPasswordChangeService(t, app, clock)
	resets := newPasswordResetService(t, app, clock)

	caller, err := sessions.Login(ctx, "reset_target", "old-password-value")
	if err != nil {
		t.Fatalf("caller sign-in: %v", err)
	}
	other, err := sessions.Login(ctx, "reset_target", "old-password-value")
	if err != nil {
		t.Fatalf("second device sign-in: %v", err)
	}
	issued, err := resets.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatalf("issue reset link: %v", err)
	}

	changed, err := change.ChangePassword(ctx, identity.ChangePasswordInput{
		Principal:       caller.Principal,
		SignInMethod:    caller.SignInMethod,
		SessionToken:    caller.Token,
		CurrentPassword: "old-password-value",
		NewPassword:     "a brand new long password",
	})
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	if changed.Token == "" || changed.Token == caller.Token {
		t.Fatal("the caller session was not rotated")
	}
	if changed.SignInMethod != identity.SignInPassword || changed.Principal.UserID != target {
		t.Fatalf("changed session=%+v", changed)
	}
	if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:a brand new long password" || version != 2 {
		t.Fatalf("stored credential hash=%q version=%d", hash, version)
	}

	for name, token := range map[string]string{"previous caller": caller.Token, "other device": other.Token} {
		if _, err = sessions.Authenticate(ctx, token); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("%s session survived the change: %v", name, err)
		}
	}
	rotated, err := sessions.Lookup(ctx, changed.Token)
	if err != nil || rotated.SignInMethod != identity.SignInPassword || rotated.Principal.UserID != target {
		t.Fatalf("rotated session=%+v err=%v", rotated, err)
	}
	if rows := countSessionsFor(t, ctx, app, target); rows != 1 {
		t.Fatalf("sessions after the change=%d, want the rotated one only", rows)
	}
	if _, err = sessions.Login(ctx, "reset_target", "old-password-value"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("old password still signs in: %v", err)
	}
	if _, err = sessions.Login(ctx, "reset_target", "a brand new long password"); err != nil {
		t.Fatalf("new password sign-in: %v", err)
	}

	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 0 {
		t.Fatalf("outstanding reset links = %d, want the change to void them", outstanding)
	}
	if _, err = resets.RedeemPasswordReset(ctx, issued.Token, "a later long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("pre-change reset link still redeems: %v", err)
	}
	if hash, _ := localCredentialState(t, ctx, admin, target); hash != "hash:a brand new long password" {
		t.Fatalf("void link wrote hash=%q", hash)
	}
}

// TestPasswordChangePostgresRejectsWithoutWriting covers every refusal that
// happens before the transaction: none of them may move the credential, rotate
// a session or sign the caller out.
func TestPasswordChangePostgresRejectsWithoutWriting(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, target, _ := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	change := newPasswordChangeService(t, app, clock)

	caller, err := sessions.Login(ctx, "reset_target", "old-password-value")
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	input := identity.ChangePasswordInput{
		Principal:       caller.Principal,
		SignInMethod:    identity.SignInPassword,
		SessionToken:    caller.Token,
		CurrentPassword: "not the old password",
		NewPassword:     "a brand new long password",
	}
	if _, err = change.ChangePassword(ctx, input); !errors.Is(err, identity.ErrCurrentPasswordIncorrect) {
		t.Fatalf("wrong current password: %v", err)
	}

	// The new password policy is checked before the current password is
	// verified, so a weak replacement is reported as a field error even though
	// the current password is wrong as well.
	weak := input
	weak.NewPassword = "short"
	var validation *identity.ValidationError
	if _, err = change.ChangePassword(ctx, weak); !errors.As(err, &validation) || validation.Field != "newPassword" || validation.Code != "invalid_length" {
		t.Fatalf("weak new password error=%v", err)
	}

	// The session kind is authoritative: an OIDC-classified request is refused
	// before either password is looked at, even with a wrong current password.
	oidc := input
	oidc.SignInMethod = identity.SignInOIDC
	if _, err = change.ChangePassword(ctx, oidc); !errors.Is(err, identity.ErrLocalSessionRequired) {
		t.Fatalf("oidc-classified change: %v", err)
	}
	malformed := input
	malformed.SessionToken = "not-a-session-cookie"
	if _, err = change.ChangePassword(ctx, malformed); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("malformed token change: %v", err)
	}

	if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:old-password-value" || version != 1 {
		t.Fatalf("refused change wrote hash=%q version=%d", hash, version)
	}
	if rows := countSessionsFor(t, ctx, app, target); rows != 1 {
		t.Fatalf("refused change touched sessions: %d", rows)
	}
	if _, err = sessions.Authenticate(ctx, caller.Token); err != nil {
		t.Fatalf("refused change signed the caller out: %v", err)
	}
}

// TestPasswordChangePostgresConcurrentChangesHaveOneWinner runs two changes of
// the same account at once. Only one may write: either the loser verified the
// previous hash before the winner committed and then finds its own session
// deleted, or it reads the new hash and the current password no longer matches.
// The credential version may advance exactly once per round either way.
func TestPasswordChangePostgresConcurrentChangesHaveOneWinner(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, target, _ := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	change := newPasswordChangeService(t, app, clock)

	current := "old-password-value"
	for round := 0; round < 3; round++ {
		replacement := fmt.Sprintf("a concurrent long password %d", round)
		caller, err := sessions.Login(ctx, "reset_target", current)
		if err != nil {
			t.Fatalf("round %d sign-in: %v", round, err)
		}
		results := make([]error, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, results[i] = change.ChangePassword(ctx, identity.ChangePasswordInput{
					Principal:       caller.Principal,
					SignInMethod:    caller.SignInMethod,
					SessionToken:    caller.Token,
					CurrentPassword: current,
					NewPassword:     replacement,
				})
			}(i)
		}
		close(start)
		wg.Wait()

		winners, losers := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				winners++
			case errors.Is(err, identity.ErrCurrentPasswordIncorrect), errors.Is(err, identity.ErrUnauthenticated):
				// A loser either verified the previous hash before the winner
				// committed and then lost the credential row, or read the new
				// hash and never matched the current password at all.
				losers++
			default:
				t.Fatalf("round %d unexpected outcome: %v", round, err)
			}
		}
		if winners != 1 || losers != 1 {
			t.Fatalf("round %d winners=%d losers=%d", round, winners, losers)
		}
		if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:"+replacement || version != int64(round)+2 {
			t.Fatalf("round %d hash=%q version=%d", round, hash, version)
		}
		if rows := countSessionsFor(t, ctx, app, target); rows != 1 {
			t.Fatalf("round %d sessions=%d, want one winner session", round, rows)
		}
		if _, err = sessions.Login(ctx, "reset_target", current); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("round %d previous password still signs in: %v", round, err)
		}
		current = replacement
	}
}

// TestPasswordChangePostgresSessionKindIsAuthoritative reads the stored kind
// back through the real lookup and proves a non-password session may never
// replace the local password, even though the account has one.
func TestPasswordChangePostgresSessionKindIsAuthoritative(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, target, _ := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	change := newPasswordChangeService(t, app, clock)
	now := clock.at

	for i, kind := range []identity.SignInMethod{identity.SignInOIDC, identity.SignInLegacy} {
		token := sessionTokenLike(byte(0x51 + i))
		if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES($1::uuid,$2,$3,$4,$5)`,
			target, sha256Sum(token), now, now.Add(24*time.Hour), string(kind)); err != nil {
			t.Fatalf("%s session insert: %v", kind, err)
		}
		info, err := sessions.Lookup(ctx, token)
		if err != nil || info.SignInMethod != kind || info.Principal.UserID != target {
			t.Fatalf("lookup %s session=%+v err=%v", kind, info, err)
		}
		_, err = change.ChangePassword(ctx, identity.ChangePasswordInput{
			Principal:       info.Principal,
			SignInMethod:    info.SignInMethod,
			SessionToken:    token,
			CurrentPassword: "old-password-value",
			NewPassword:     "a brand new long password",
		})
		if !errors.Is(err, identity.ErrLocalSessionRequired) {
			t.Fatalf("%s session change: %v", kind, err)
		}
		if _, err = sessions.Authenticate(ctx, token); err != nil {
			t.Fatalf("%s session lost by a refused change: %v", kind, err)
		}
	}

	if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:old-password-value" || version != 1 {
		t.Fatalf("refused changes wrote hash=%q version=%d", hash, version)
	}
	// An unclassified session insert is a programming fault, never a row.
	repository := NewPasswordChangeRepository(app)
	err := repository.WithPasswordChangeTransaction(ctx, func(tx identity.PasswordChangeTransaction) error {
		_, insertErr := tx.CreateSession(ctx, identity.NewSession{
			UserID: target, TokenHash: sha256Sum("unclassified"), CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		return insertErr
	})
	if !errors.Is(err, identity.ErrInvalidSignInMethod) {
		t.Fatalf("unclassified session insert: %v", err)
	}
	var unclassified int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE token_hash=$1`, sha256Sum("unclassified")).Scan(&unclassified); err != nil || unclassified != 0 {
		t.Fatalf("unclassified session rows=%d err=%v", unclassified, err)
	}
}

// isDeadlockError reports a PostgreSQL deadlock (40P01). The change, the reset
// issuance and the reset redemption share rows, so a wrong lock mode must show
// up as this code rather than as a generic persistence failure.
func isDeadlockError(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40P01"
}

// TestPasswordChangePostgresDoesNotDeadlockWithResetIssuance pins the lock order
// every credential path shares (ADR 0018): user_accounts FOR NO KEY UPDATE ->
// password_reset_tokens -> local_credentials -> user_sessions.
//
// A change revokes the account's outstanding reset links, locks the credential,
// deletes the old sessions and then inserts the replacement session, and that
// insert takes the foreign-key KEY SHARE on the account row. Issuing a link
// locks the account row first and then revokes the same link rows. Taking the
// account row FOR UPDATE closes a cycle between the two: the issuance holds the
// account row and waits for the link row the change already locked, while the
// change waits for the key share. PostgreSQL then aborts one side with 40P01,
// which the HTTP layer can only report as 503. FOR NO KEY UPDATE still
// serializes two issuances for one target but never conflicts with the key
// share, so the change finishes while the issuance is still holding the account
// lock and only the issuance waits.
func TestPasswordChangePostgresDoesNotDeadlockWithResetIssuance(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	change := newPasswordChangeService(t, app, clock)
	resets := newPasswordResetService(t, app, clock)

	caller, err := sessions.Login(ctx, "reset_target", "old-password-value")
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	issued, err := resets.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatalf("issue reset link: %v", err)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 1 {
		t.Fatalf("fixture must start with one outstanding link, got %d", outstanding)
	}

	// The issuance side replays the two statements of IssuePasswordReset through
	// the same repository protocol: authorizePasswordResetIssue locks the target
	// account row, IssuePasswordReset revokes the previous link. It pauses
	// between them so the change is guaranteed to run against a held account
	// lock instead of racing it.
	repository := NewPasswordResetRepository(app, passwordResetFactory, clock.now)
	accountLocked := make(chan struct{})
	revokeNow := make(chan struct{})
	issuanceDone := make(chan struct{})
	var issuanceErr error
	go func() {
		defer close(issuanceDone)
		issuanceErr = repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
			if _, e := tx.LockPasswordResetTarget(ctx, target); e != nil {
				return e
			}
			close(accountLocked)
			<-revokeNow
			return tx.RevokeOutstandingPasswordResets(ctx, target, clock.at)
		})
	}()
	<-accountLocked

	var changed identity.Session
	var changeErr error
	changeFinished := make(chan struct{})
	go func() {
		defer close(changeFinished)
		changed, changeErr = change.ChangePassword(ctx, identity.ChangePasswordInput{
			Principal:       caller.Principal,
			SignInMethod:    caller.SignInMethod,
			SessionToken:    caller.Token,
			CurrentPassword: "old-password-value",
			NewPassword:     "a brand new long password",
		})
	}()

	// The change must finish while the issuance still holds the account row
	// lock. With FOR NO KEY UPDATE the key share taken by the session insert is
	// compatible, so nothing on the change side waits for the issuance. If this
	// times out the account lock blocks session insertion again; the issuance is
	// then released so the blocked change can resolve before the test fails.
	select {
	case <-changeFinished:
	case <-time.After(5 * time.Second):
		close(revokeNow)
		<-issuanceDone
		<-changeFinished
		t.Fatalf("change blocked behind issuance's account-row lock: issuance=%v change=%v", issuanceErr, changeErr)
	}

	close(revokeNow)
	<-issuanceDone
	<-changeFinished

	if isDeadlockError(issuanceErr) || isDeadlockError(changeErr) {
		t.Fatalf("change and reset issuance deadlocked: issuance=%v change=%v", issuanceErr, changeErr)
	}
	if issuanceErr != nil {
		t.Fatalf("reset issuance failed: %v", issuanceErr)
	}
	if changeErr != nil {
		t.Fatalf("change failed: %v", changeErr)
	}
	if changed.Token == "" || changed.Token == caller.Token || changed.Principal.UserID != target {
		t.Fatalf("changed session=%+v", changed)
	}
	if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:a brand new long password" || version != 2 {
		t.Fatalf("stored credential hash=%q version=%d", hash, version)
	}
	if rows := countSessionsFor(t, ctx, app, target); rows != 1 {
		t.Fatalf("sessions after the change=%d, want the rotated one only", rows)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 0 {
		t.Fatalf("outstanding reset links after the change=%d", outstanding)
	}
	if _, err = resets.RedeemPasswordReset(ctx, issued.Token, "a later long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("link issued before the change still redeems: %v", err)
	}
}

// TestAdminPasswordPostgresResetIsTheOperatorContract covers the CLI reset: the
// new hash signs in, the old one does not, every session is gone, outstanding
// admin reset links are void, and only unexpired sessions are reported.
func TestAdminPasswordPostgresResetIsTheOperatorContract(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	resets := newPasswordResetService(t, app, clock)
	operator := newAdminPasswordService(t, app, clock)

	live := []identity.Session{}
	for i := 0; i < 2; i++ {
		session, err := sessions.Login(ctx, "reset_target", "old-password-value")
		if err != nil {
			t.Fatalf("sign-in %d: %v", i, err)
		}
		live = append(live, session)
	}
	// An already expired session is deleted with the rest but must not be
	// reported as a sign-out the operator caused.
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES($1::uuid,$2,$3,$4,'password')`,
		target, sha256Sum("expired-session"), clock.at.Add(-2*time.Hour), clock.at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	issued, err := resets.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatalf("issue reset link: %v", err)
	}

	reset, err := operator.SetPassword(ctx, "reset_target", "an operator chosen password")
	if err != nil {
		t.Fatalf("operator reset: %v", err)
	}
	if reset.UserID != target || reset.Sessions != 2 {
		t.Fatalf("operator reset report=%+v, want two unexpired sign-outs", reset)
	}
	if hash, version := localCredentialState(t, ctx, admin, target); hash != "hash:an operator chosen password" || version != 2 {
		t.Fatalf("stored credential hash=%q version=%d", hash, version)
	}
	if rows := countSessionsFor(t, ctx, app, target); rows != 0 {
		t.Fatalf("sessions after the operator reset=%d", rows)
	}
	for i, session := range live {
		if _, err = sessions.Authenticate(ctx, session.Token); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("session %d survived the operator reset: %v", i, err)
		}
	}
	if _, err = sessions.Login(ctx, "reset_target", "old-password-value"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("old password still signs in: %v", err)
	}
	if _, err = sessions.Login(ctx, "reset_target", "an operator chosen password"); err != nil {
		t.Fatalf("new password sign-in: %v", err)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 0 {
		t.Fatalf("outstanding reset links after the operator reset=%d", outstanding)
	}
	if _, err = resets.RedeemPasswordReset(ctx, issued.Token, "a later long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("pre-reset link still redeems: %v", err)
	}

	// The operator authority is host access, not membership: an account outside
	// every household can still be reset, and it reports no sign-outs.
	outsider := insertAccount(t, ctx, admin, "operator_outsider")
	if _, err = admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'hash:old-password-value')`, outsider); err != nil {
		t.Fatal(err)
	}
	reset, err = operator.SetPassword(ctx, "operator_outsider", "another operator password")
	if err != nil || reset.UserID != outsider || reset.Sessions != 0 {
		t.Fatalf("memberless reset=%+v err=%v", reset, err)
	}
	if hash, _ := localCredentialState(t, ctx, admin, outsider); hash != "hash:another operator password" {
		t.Fatalf("memberless reset hash=%q", hash)
	}
}

// TestAdminPasswordPostgresRefusals pins the CLI's fail-closed answers: the
// policy is checked before the account lookup, an unknown login stays
// indistinguishable, and an OIDC-only account never gains a local password.
func TestAdminPasswordPostgresRefusals(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)}
	sessions := newSessionService(t, app, clock)
	operator := newAdminPasswordService(t, app, clock)

	var validation *identity.ValidationError
	if _, err := operator.SetPassword(ctx, "operator_missing", "short"); !errors.As(err, &validation) || validation.Field != "password" || validation.Code != "invalid_length" {
		t.Fatalf("weak password for an unknown login error=%v", err)
	}
	if _, err := operator.SetPassword(ctx, "operator_missing", "a long enough password"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown login error=%v", err)
	}
	for _, login := range []string{"", "bad login", "Upper_case", strings.Repeat("a", 65)} {
		if _, err := operator.SetPassword(ctx, login, "a long enough password"); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("malformed login %q error=%v", login, err)
		}
	}

	var oidcOnly string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('operator_oidc_only') RETURNING id::text`).Scan(&oidcOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES('https://idp.example','operator-oidc-subject',$1::uuid,now())`, oidcOnly); err != nil {
		t.Fatal(err)
	}
	token := sessionTokenLike(0x61)
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES($1::uuid,$2,$3,$4,'oidc')`,
		oidcOnly, sha256Sum(token), clock.at, clock.at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.SetPassword(ctx, "operator_oidc_only", "an operator chosen password"); !errors.Is(err, identity.ErrNoLocalPassword) {
		t.Fatalf("oidc-only reset error=%v", err)
	}
	var credentials int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM local_credentials WHERE user_id=$1::uuid`, oidcOnly).Scan(&credentials); err != nil || credentials != 0 {
		t.Fatalf("oidc-only reset created %d local credentials (err=%v)", credentials, err)
	}
	if _, err := sessions.Authenticate(ctx, token); err != nil {
		t.Fatalf("refused oidc-only reset signed the account out: %v", err)
	}
}
