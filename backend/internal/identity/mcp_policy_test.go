package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

var (
	mcpBoth     = []string{MCPScopeRead, MCPScopeWrite}
	mcpReadOnly = []string{MCPScopeRead}
	mcpWriteOne = []string{MCPScopeWrite}
)

func mcpBool(v bool) *bool { return &v }

// TestMCPPolicyIntersectionMatrix enumerates every combination of installation
// default, account override, user setting and grant scopes. The effective set
// is never larger than any single term: no override elevates.
func TestMCPPolicyIntersectionMatrix(t *testing.T) {
	scopeSets := [][]string{mcpReadOnly, mcpWriteOne, mcpBoth}
	optionalSets := append([][]string{nil}, scopeSets...)
	for _, installationEnabled := range []bool{false, true} {
		for _, defaultEnabled := range []bool{false, true} {
			for _, adminEnabled := range []*bool{nil, mcpBool(false), mcpBool(true)} {
				for _, userEnabled := range []bool{false, true} {
					for _, defaultMax := range scopeSets {
						for _, adminMax := range optionalSets {
							for _, userScopes := range scopeSets {
								for _, grant := range scopeSets {
									p := MCPPolicyRecord{
										InstallationFound: true, InstallationEnabled: installationEnabled,
										DefaultAccountEnabled: defaultEnabled, DefaultMaxScopes: defaultMax,
										AccountFound: true, AdminEnabled: adminEnabled, AdminMaxScopes: adminMax,
										UserEnabled: userEnabled, UserScopes: userScopes,
									}
									got := p.Effective(grant)
									accountOn := defaultEnabled
									if adminEnabled != nil {
										accountOn = *adminEnabled
									}
									maximum := defaultMax
									if adminMax != nil {
										maximum = adminMax
									}
									var want []string
									if installationEnabled && accountOn && userEnabled {
										for _, s := range []string{MCPScopeRead, MCPScopeWrite} {
											if slices.Contains(maximum, s) && slices.Contains(userScopes, s) && slices.Contains(grant, s) {
												want = append(want, s)
											}
										}
									}
									name := fmt.Sprintf("%+v grant=%v", p, grant)
									if !slices.Equal(got, want) {
										t.Fatalf("%s: effective=%v want %v", name, got, want)
									}
									for _, s := range got {
										if !slices.Contains(grant, s) || !slices.Contains(userScopes, s) || !slices.Contains(maximum, s) {
											t.Fatalf("%s: %s elevated beyond a term", name, s)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// TestMCPPolicyMissingRowsAreInactive proves a missing installation or account
// row never falls back to a default, and unknown stored scopes are dropped.
func TestMCPPolicyMissingRowsAreInactive(t *testing.T) {
	missingInstallation := mcpDefaultPolicy()
	missingInstallation.InstallationFound = false
	if missingInstallation.InstallationActive() || missingInstallation.Effective(mcpBoth) != nil {
		t.Fatal("missing installation row evaluated as active")
	}
	missingAccount := mcpDefaultPolicy()
	missingAccount.AccountFound = false
	if !missingAccount.InstallationActive() || missingAccount.Effective(mcpBoth) != nil {
		t.Fatal("missing account row evaluated as allowed")
	}
	unknown := mcpDefaultPolicy()
	unknown.UserScopes = []string{"tendo:admin", MCPScopeRead}
	if got := unknown.Effective([]string{"tendo:admin", MCPScopeRead, MCPScopeWrite}); !slices.Equal(got, mcpReadOnly) {
		t.Fatalf("unknown scope survived: %v", got)
	}
}

// mcpPolicyCase names one stored policy that leaves the grant no scope.
type mcpPolicyCase struct {
	name   string
	policy func(*MCPPolicyRecord)
}

var mcpPausingPolicies = []mcpPolicyCase{
	{"account default off", func(p *MCPPolicyRecord) { p.DefaultAccountEnabled = false }},
	{"admin account off", func(p *MCPPolicyRecord) { p.AdminEnabled = mcpBool(false) }},
	{"user off", func(p *MCPPolicyRecord) { p.UserEnabled = false }},
	{"missing account row", func(p *MCPPolicyRecord) { p.AccountFound = false }},
	{"admin maximum excludes grant", func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpWriteOne }},
	{"user scopes exclude grant", func(p *MCPPolicyRecord) { p.UserScopes = mcpWriteOne }},
}

var mcpInstallationOff = []mcpPolicyCase{
	{"installation off", func(p *MCPPolicyRecord) { p.InstallationEnabled = false }},
	{"missing installation row", func(p *MCPPolicyRecord) { p.InstallationFound = false }},
}

func (fx *mcpFixture) setPolicy(change func(*MCPPolicyRecord)) {
	p := mcpDefaultPolicy()
	if change != nil {
		change(&p)
	}
	fx.f.policy = p
}

// TestMCPVerifyAppliesPolicyOnEveryRequest: a paused account or user is refused
// on the next request with invalid_token, the installation switch answers as
// MCP disabled, and re-enabling revives the same token: pausing never revokes.
func TestMCPVerifyAppliesPolicyOnEveryRequest(t *testing.T) {
	for _, tc := range append(slices.Clone(mcpPausingPolicies), mcpInstallationOff...) {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			_, tokens := fx.exchangeOK() // grant scopes: read
			if _, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken); err != nil {
				t.Fatalf("verify before change: %v", err)
			}
			fx.setPolicy(tc.policy)
			want := ErrMCPInvalidToken
			if slices.ContainsFunc(mcpInstallationOff, func(c mcpPolicyCase) bool { return c.name == tc.name }) {
				want = ErrMCPDisabled
			}
			if _, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken); !errors.Is(err, want) {
				t.Fatalf("verify after change: err=%v want %v", err, want)
			}
			if len(fx.f.liveGrantIDs()) != 1 || len(fx.f.state.revocations) != 0 {
				t.Fatal("a policy change revoked the grant")
			}
			fx.setPolicy(nil)
			access, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken)
			if err != nil || !slices.Equal(access.Scopes, mcpReadOnly) {
				t.Fatalf("verify after re-enable: %+v %v", access, err)
			}
		})
	}
}

// TestMCPVerifyReturnsEffectiveScopes: a read+write grant under a read-only
// maximum is admitted with read only.
func TestMCPVerifyReturnsEffectiveScopes(t *testing.T) {
	for _, tc := range []mcpPolicyCase{
		{"admin maximum read", func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly }},
		{"installation default read", func(p *MCPPolicyRecord) { p.DefaultMaxScopes = mcpReadOnly }},
		{"user narrows to read", func(p *MCPPolicyRecord) { p.UserScopes = mcpReadOnly }},
		{"user write cannot elevate past admin read", func(p *MCPPolicyRecord) {
			p.AdminMaxScopes = mcpReadOnly
			p.UserScopes = mcpBoth
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			in := mcpValidAuthorization()
			in.Scopes = mcpBoth
			tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(fx.issueCode(in)))
			if err != nil {
				t.Fatal(err)
			}
			fx.setPolicy(tc.policy)
			access, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken)
			if err != nil || !slices.Equal(access.Scopes, mcpReadOnly) {
				t.Fatalf("access=%+v err=%v want read only", access, err)
			}
		})
	}
}

// TestMCPExchangeRefusedWhilePaused: an approved code cannot be redeemed while
// the account or user is paused, and the installation switch answers disabled.
// The refusal writes nothing, so the code is still unused.
func TestMCPExchangeRefusedWhilePaused(t *testing.T) {
	for _, tc := range append(slices.Clone(mcpPausingPolicies), mcpInstallationOff...) {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			code := fx.issueCode(mcpValidAuthorization())
			fx.setPolicy(tc.policy)
			want := ErrMCPInvalidGrant
			if slices.ContainsFunc(mcpInstallationOff, func(c mcpPolicyCase) bool { return c.name == tc.name }) {
				want = ErrMCPDisabled
			}
			if _, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code)); !errors.Is(err, want) {
				t.Fatalf("exchange: err=%v want %v", err, want)
			}
			if len(fx.f.state.grants) != 0 || len(fx.f.state.access) != 0 {
				t.Fatal("a refused exchange created a grant or token")
			}
			if fx.f.state.codes[string(mcpDigest(code))].Used {
				t.Fatal("a refused exchange used the code")
			}
		})
	}
}

// TestMCPExchangeAnswersEffectiveScopes: the grant keeps the consented scopes;
// the token response names the ones the policy allows now.
func TestMCPExchangeAnswersEffectiveScopes(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = mcpBoth
	code := fx.issueCode(in)
	fx.setPolicy(func(p *MCPPolicyRecord) { p.UserScopes = mcpReadOnly })
	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
	if err != nil || !slices.Equal(tokens.Scopes, mcpReadOnly) {
		t.Fatalf("tokens=%v err=%v", tokens.Scopes, err)
	}
	if got := fx.f.state.grants[fx.f.onlyGrantID(t)].Scopes; !slices.Equal(got, mcpBoth) {
		t.Fatalf("grant scopes=%v want the consented set", got)
	}
}

// TestMCPRefreshRefusedWhilePausedWithoutConsuming: refresh is refused, the
// token stays unconsumed and refreshes once access is re-enabled.
func TestMCPRefreshRefusedWhilePausedWithoutConsuming(t *testing.T) {
	for _, tc := range append(slices.Clone(mcpPausingPolicies), mcpInstallationOff...) {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			_, tokens := fx.exchangeOK()
			fx.setPolicy(tc.policy)
			want := ErrMCPInvalidGrant
			if slices.ContainsFunc(mcpInstallationOff, func(c mcpPolicyCase) bool { return c.name == tc.name }) {
				want = ErrMCPDisabled
			}
			if _, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens)); !errors.Is(err, want) {
				t.Fatalf("refresh: err=%v want %v", err, want)
			}
			if fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))].Consumed {
				t.Fatal("a refused refresh consumed the token")
			}
			if len(fx.f.state.revocations) != 0 {
				t.Fatal("a refused refresh revoked the grant")
			}
			fx.setPolicy(nil)
			if _, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens)); err != nil {
				t.Fatalf("refresh after re-enable: %v", err)
			}
		})
	}
}

// TestMCPRefreshReplayRevokesWhilePaused: replay detection runs before the
// policy, so a replayed token still revokes the grant as replay.
func TestMCPRefreshReplayRevokesWhilePaused(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	if _, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens)); err != nil {
		t.Fatal(err)
	}
	fx.setPolicy(func(p *MCPPolicyRecord) { p.AdminEnabled = mcpBool(false) })
	if _, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens)); !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("replay: err=%v", err)
	}
	if got := fx.f.state.revocations[fx.f.onlyGrantID(t)].Reason; got != MCPRevokeReplay {
		t.Fatalf("revocation=%q want replay", got)
	}
}

// TestMCPRefreshAnswersEffectiveScopes: a read+write grant refreshes under a
// read-only maximum and is answered with read only; the grant is unchanged.
func TestMCPRefreshAnswersEffectiveScopes(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = mcpBoth
	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(fx.issueCode(in)))
	if err != nil {
		t.Fatal(err)
	}
	fx.setPolicy(func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly })
	next, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens))
	if err != nil || !slices.Equal(next.Scopes, mcpReadOnly) {
		t.Fatalf("scopes=%v err=%v", next.Scopes, err)
	}
	if got := fx.f.state.grants[fx.f.onlyGrantID(t)].Scopes; !slices.Equal(got, mcpBoth) {
		t.Fatalf("grant scopes=%v", got)
	}
}

// TestMCPConsentIssuesRequestedIntersectAllowed: the approved code carries the
// requested scopes the policy allows; with none allowed the approval is
// answered as access_denied (no code) and the pending consent is consumed.
func TestMCPConsentIssuesRequestedIntersectAllowed(t *testing.T) {
	t.Run("narrowed", func(t *testing.T) {
		fx := newMCPFixture(t)
		params := mcpValidParams()
		params.Scopes = mcpBoth
		pending, err := fx.svc.BeginAuthorization(context.Background(), params)
		if err != nil {
			t.Fatal(err)
		}
		fx.setPolicy(func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly })
		result, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true))
		if err != nil || result.Code == "" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if got := fx.f.state.codes[string(mcpDigest(result.Code))].Scopes; !slices.Equal(got, mcpReadOnly) {
			t.Fatalf("code scopes=%v want read", got)
		}
	})
	for _, tc := range mcpPausingPolicies {
		t.Run("denied "+tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			pending := mcpBeginOK(t, fx) // requests read
			fx.setPolicy(tc.policy)
			result, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true))
			if err != nil || result.Code != "" || result.RedirectURI != mcpTestRedirect || result.State != "state-1" {
				t.Fatalf("result=%+v err=%v want access_denied redirect", result, err)
			}
			if len(fx.f.state.codes) != 0 || fx.f.requestCount() != 0 {
				t.Fatal("denied approval stored a code or kept the consent")
			}
		})
	}
	for _, tc := range mcpInstallationOff {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			pending := mcpBeginOK(t, fx)
			fx.setPolicy(tc.policy)
			if _, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true)); !errors.Is(err, ErrMCPDisabled) {
				t.Fatalf("err=%v want ErrMCPDisabled", err)
			}
			if len(fx.f.state.codes) != 0 || fx.f.requestCount() != 1 {
				t.Fatal("disabled decision stored a code or consumed the consent")
			}
		})
	}
}

// TestMCPConsentDecisionNeverExceedsWhatWasShown (B4b1): the approval carries
// the scopes the consent screen showed, and the code never holds more, even
// when the policy widened between the view and the decision. Without them the
// decision issues requested ∩ allowed, as before.
func TestMCPConsentDecisionNeverExceedsWhatWasShown(t *testing.T) {
	cases := []struct {
		name   string
		policy func(*MCPPolicyRecord)
		shown  []string
		want   []string
	}{
		{"policy widened after the view", nil, mcpReadOnly, mcpReadOnly},
		{"policy narrowed after the view", func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly }, mcpBoth, mcpReadOnly},
		{"nothing shown is still allowed", func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly }, mcpWriteOne, nil},
		{"not sent", nil, nil, mcpBoth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			params := mcpValidParams()
			params.Scopes = mcpBoth
			pending, err := fx.svc.BeginAuthorization(context.Background(), params)
			if err != nil {
				t.Fatal(err)
			}
			fx.setPolicy(tc.policy)
			in := mcpDecisionOf(pending, true)
			in.ShownScopes = tc.shown
			result, err := fx.svc.DecideAuthorization(context.Background(), in)
			if err != nil || result.RedirectURI != mcpTestRedirect || result.State != "state-1" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if tc.want == nil {
				if result.Code != "" || len(fx.f.state.codes) != 0 {
					t.Fatalf("issued a code for nothing shown and allowed: %+v", result)
				}
				return
			}
			if got := fx.f.state.codes[string(mcpDigest(result.Code))].Scopes; !slices.Equal(got, tc.want) {
				t.Fatalf("code scopes=%v want %v", got, tc.want)
			}
		})
	}
}

// TestMCPDirectIssuanceAppliesPolicy covers IssueAuthorizationCode, the
// issuance path without a pending consent.
func TestMCPDirectIssuanceAppliesPolicy(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = mcpBoth
	fx.setPolicy(func(p *MCPPolicyRecord) { p.UserScopes = mcpWriteOne })
	code := fx.issueCode(in)
	if got := fx.f.state.codes[string(mcpDigest(code))].Scopes; !slices.Equal(got, mcpWriteOne) {
		t.Fatalf("code scopes=%v want write", got)
	}
	fx.setPolicy(func(p *MCPPolicyRecord) { p.UserEnabled = false })
	if _, err := fx.svc.IssueAuthorizationCode(context.Background(), in); !errors.Is(err, ErrMCPAccessDenied) {
		t.Fatalf("paused issuance err=%v", err)
	}
	fx.setPolicy(func(p *MCPPolicyRecord) { p.InstallationEnabled = false })
	if _, err := fx.svc.IssueAuthorizationCode(context.Background(), in); !errors.Is(err, ErrMCPDisabled) {
		t.Fatalf("installation off issuance err=%v", err)
	}
}

// TestMCPRefreshAcceptsTheAnsweredScope: once the policy narrows a grant, the
// client may send back the scope the last token response named; any other
// subset is still refused without consuming the token.
func TestMCPRefreshAcceptsTheAnsweredScope(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = mcpBoth
	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(fx.issueCode(in)))
	if err != nil {
		t.Fatal(err)
	}
	fx.setPolicy(func(p *MCPPolicyRecord) { p.UserScopes = mcpReadOnly })
	wrong := mcpRefreshOf(tokens)
	wrong.Scopes = mcpWriteOne
	if _, err := fx.svc.Refresh(context.Background(), wrong); !errors.Is(err, ErrMCPInvalidScope) {
		t.Fatalf("other subset err=%v want ErrMCPInvalidScope", err)
	}
	if fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))].Consumed {
		t.Fatal("a refused scope consumed the token")
	}
	answered := mcpRefreshOf(tokens)
	answered.Scopes = mcpReadOnly
	next, err := fx.svc.Refresh(context.Background(), answered)
	if err != nil || !slices.Equal(next.Scopes, mcpReadOnly) {
		t.Fatalf("answered scope: %v %v", next.Scopes, err)
	}
	consented := mcpRefreshOf(next)
	consented.Scopes = mcpBoth
	if _, err := fx.svc.Refresh(context.Background(), consented); err != nil {
		t.Fatalf("consented scope: %v", err)
	}
}

// TestMCPRefreshInstallationOffBeforeScope: with the installation off, a
// mismatched scope still answers disabled, never invalid_scope.
func TestMCPRefreshInstallationOffBeforeScope(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	fx.setPolicy(func(p *MCPPolicyRecord) { p.InstallationEnabled = false })
	req := mcpRefreshOf(tokens)
	req.Scopes = mcpBoth
	if _, err := fx.svc.Refresh(context.Background(), req); !errors.Is(err, ErrMCPDisabled) {
		t.Fatalf("err=%v want ErrMCPDisabled", err)
	}
}

// TestMCPConsentViewNamesAllowedScopes: the consent screen view carries the
// requested scopes the signed-in account's policy allows now, never wider than
// the request; a paused account or user allows none; an installation that is
// switched off answers as MCP disabled. Reading never consumes the consent.
func TestMCPConsentViewNamesAllowedScopes(t *testing.T) {
	cases := []struct {
		name      string
		requested []string
		policy    func(*MCPPolicyRecord)
		want      []string
	}{
		{"default allows the request", mcpBoth, nil, mcpBoth},
		{"never wider than the request", mcpReadOnly, nil, mcpReadOnly},
		{"admin maximum narrows", mcpBoth, func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpReadOnly }, mcpReadOnly},
		{"installation default narrows", mcpBoth, func(p *MCPPolicyRecord) { p.DefaultMaxScopes = mcpWriteOne }, mcpWriteOne},
		{"own settings narrow", mcpBoth, func(p *MCPPolicyRecord) { p.UserScopes = mcpReadOnly }, mcpReadOnly},
		{"admin override elevates nothing", mcpBoth, func(p *MCPPolicyRecord) {
			p.DefaultMaxScopes, p.AdminMaxScopes, p.UserScopes = mcpBoth, mcpBoth, mcpWriteOne
		}, mcpWriteOne},
		{"disjoint", mcpReadOnly, func(p *MCPPolicyRecord) { p.AdminMaxScopes = mcpWriteOne }, nil},
	}
	for _, tc := range mcpPausingPolicies {
		cases = append(cases, struct {
			name      string
			requested []string
			policy    func(*MCPPolicyRecord)
			want      []string
		}{"blocked " + tc.name, mcpReadOnly, tc.policy, nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			params := mcpValidParams()
			params.Scopes = tc.requested
			pending, err := fx.svc.BeginAuthorization(context.Background(), params)
			if err != nil {
				t.Fatal(err)
			}
			fx.setPolicy(tc.policy)
			view, err := fx.svc.GetAuthorizationRequest(context.Background(), mcpTestUser, pending.Handle, pending.BrowserBinding)
			if err != nil {
				t.Fatalf("GetAuthorizationRequest: %v", err)
			}
			if !slices.Equal(view.Scopes, tc.requested) || !slices.Equal(view.AllowedScopes, tc.want) {
				t.Fatalf("scopes=%v allowed=%v want %v and %v", view.Scopes, view.AllowedScopes, tc.requested, tc.want)
			}
			if fx.f.requestCount() != 1 {
				t.Fatal("reading the consent consumed it")
			}
		})
	}
	for _, tc := range mcpInstallationOff {
		t.Run(tc.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			pending := mcpBeginOK(t, fx)
			fx.setPolicy(tc.policy)
			if _, err := fx.svc.GetAuthorizationRequest(context.Background(), mcpTestUser, pending.Handle, pending.BrowserBinding); !errors.Is(err, ErrMCPDisabled) {
				t.Fatalf("err=%v want ErrMCPDisabled", err)
			}
		})
	}
	t.Run("no signed-in account", func(t *testing.T) {
		fx := newMCPFixture(t)
		pending := mcpBeginOK(t, fx)
		fx.f.calls = nil
		if _, err := fx.svc.GetAuthorizationRequest(context.Background(), "", pending.Handle, pending.BrowserBinding); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("err=%v want ErrUnauthenticated", err)
		}
		if len(fx.f.calls) != 0 {
			t.Fatalf("anonymous read touched the repository: %v", fx.f.calls)
		}
	})
}
