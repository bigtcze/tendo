package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	mcpTestResource  = "https://tendo.test/mcp"
	mcpTestUser      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
	mcpTestClient    = "mcp-client-1"
	mcpTestRedirect  = "https://client.test/callback"
	mcpTestSession   = "session-token-for-mcp-tests"
	mcpTestVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	mcpTestChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

var mcpTestStart = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

var errMCPInjected = errors.New("injected infrastructure failure")

// mcpFake is an in-memory MCPRepository. WithMCPTransaction runs work against
// a copy of the committed state and publishes that copy only when work returns
// nil, so a refusal that returns an error rolls back, while a refusal that
// returns nil (the replay and code-reuse revocations) commits. Every operation
// appends its name to calls, so tests can assert lock order.
type mcpFake struct {
	mu  sync.Mutex
	now time.Time

	calls []string
	// failOn names an operation that returns errMCPInjected.
	failOn        string
	findActiveErr error

	accounts      map[string]bool
	sessionActive bool
	clients       map[string]MCPClient

	state mcpState
}

// mcpState is the committed database state.
type mcpState struct {
	codes       map[string]MCPCodeRecord
	grants      map[string]MCPGrantRecord
	access      map[string]MCPTokenIssue
	refresh     map[string]MCPRefreshRecord
	revocations map[string]MCPRevocation
	// requests are pending consents keyed by handle digest.
	requests  map[string]MCPAuthorizationRequestRecord
	nextGrant int
}

func newMCPFake(now time.Time) *mcpFake {
	return &mcpFake{
		now:           now,
		accounts:      map[string]bool{mcpTestUser: true},
		sessionActive: true,
		clients: map[string]MCPClient{
			mcpTestClient: {ID: mcpTestClient, Name: "Test client", RedirectURIs: []string{mcpTestRedirect}},
		},
		state: mcpState{
			codes:       map[string]MCPCodeRecord{},
			grants:      map[string]MCPGrantRecord{},
			access:      map[string]MCPTokenIssue{},
			refresh:     map[string]MCPRefreshRecord{},
			revocations: map[string]MCPRevocation{},
			requests:    map[string]MCPAuthorizationRequestRecord{},
		},
	}
}

func (s mcpState) clone() mcpState {
	return mcpState{
		codes:       maps.Clone(s.codes),
		grants:      maps.Clone(s.grants),
		access:      maps.Clone(s.access),
		refresh:     maps.Clone(s.refresh),
		revocations: maps.Clone(s.revocations),
		requests:    maps.Clone(s.requests),
		nextGrant:   s.nextGrant,
	}
}

// clock is read outside any transaction; tests advance f.now directly.
func (f *mcpFake) clock() time.Time { return f.now }

func (f *mcpFake) WithMCPTransaction(ctx context.Context, work func(MCPTransaction) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "WithMCPTransaction")
	tx := &mcpTx{f: f, s: f.state.clone()}
	if err := work(tx); err != nil {
		return err // rollback: the working copy is discarded
	}
	f.state = tx.s // commit
	return nil
}

func (f *mcpFake) FindActiveAccessToken(ctx context.Context, tokenHash []byte, now time.Time) (MCPGrantRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "FindActiveAccessToken")
	if f.findActiveErr != nil {
		return MCPGrantRecord{}, f.findActiveErr
	}
	row, ok := f.state.access[string(tokenHash)]
	if !ok || !row.AccessExpiresAt.After(now) {
		return MCPGrantRecord{}, ErrNotFound
	}
	grant := f.state.grants[row.GrantID]
	if grant.Revoked || !grant.ExpiresAt.After(now) || !f.accounts[grant.UserID] {
		return MCPGrantRecord{}, ErrNotFound
	}
	return grant, nil
}

// mcpTx works on a private copy of the state. Its methods do not lock: the
// enclosing WithMCPTransaction already holds f.mu.
type mcpTx struct {
	f *mcpFake
	s mcpState
}

func (t *mcpTx) op(name string) error {
	t.f.calls = append(t.f.calls, name)
	if t.f.failOn == name {
		return errMCPInjected
	}
	return nil
}

func (t *mcpTx) LockLiveAccount(ctx context.Context, userID string) error {
	if err := t.op("LockLiveAccount"); err != nil {
		return err
	}
	if !t.f.accounts[userID] {
		return ErrNotFound
	}
	return nil
}

func (t *mcpTx) ActiveSessionExists(ctx context.Context, userID string, tokenHash []byte, now time.Time) (bool, error) {
	if err := t.op("ActiveSessionExists"); err != nil {
		return false, err
	}
	return t.f.sessionActive, nil
}

func (t *mcpTx) FindClient(ctx context.Context, clientID string) (MCPClient, error) {
	if err := t.op("FindClient"); err != nil {
		return MCPClient{}, err
	}
	client, ok := t.f.clients[clientID]
	if !ok {
		return MCPClient{}, ErrNotFound
	}
	return client, nil
}

func (t *mcpTx) CreateAuthorizationCode(ctx context.Context, code MCPCodeRecord) error {
	if err := t.op("CreateAuthorizationCode"); err != nil {
		return err
	}
	t.s.codes[string(code.CodeHash)] = code
	return nil
}

func (t *mcpTx) FindCodeOwner(ctx context.Context, codeHash []byte) (string, error) {
	if err := t.op("FindCodeOwner"); err != nil {
		return "", err
	}
	code, ok := t.s.codes[string(codeHash)]
	if !ok {
		return "", ErrNotFound
	}
	return code.UserID, nil
}

func (t *mcpTx) LockAuthorizationCode(ctx context.Context, codeHash []byte) (MCPCodeRecord, error) {
	if err := t.op("LockAuthorizationCode"); err != nil {
		return MCPCodeRecord{}, err
	}
	code, ok := t.s.codes[string(codeHash)]
	if !ok {
		return MCPCodeRecord{}, ErrNotFound
	}
	return code, nil
}

func (t *mcpTx) MarkAuthorizationCodeUsed(ctx context.Context, codeHash []byte, grantID string, now time.Time) error {
	if err := t.op("MarkAuthorizationCodeUsed"); err != nil {
		return err
	}
	code, ok := t.s.codes[string(codeHash)]
	if !ok {
		return ErrNotFound
	}
	code.Used = true
	code.IssuedGrantID = grantID
	t.s.codes[string(codeHash)] = code
	return nil
}

func (t *mcpTx) CreateGrant(ctx context.Context, grant MCPGrantRecord) (string, error) {
	if err := t.op("CreateGrant"); err != nil {
		return "", err
	}
	t.s.nextGrant++
	grant.ID = fmt.Sprintf("grant-%d", t.s.nextGrant)
	t.s.grants[grant.ID] = grant
	return grant.ID, nil
}

func (t *mcpTx) CreateTokens(ctx context.Context, issue MCPTokenIssue) error {
	if err := t.op("CreateTokens"); err != nil {
		return err
	}
	t.s.access[string(issue.AccessHash)] = issue
	t.s.refresh[string(issue.RefreshHash)] = MCPRefreshRecord{GrantID: issue.GrantID, ExpiresAt: issue.RefreshExpiresAt}
	return nil
}

func (t *mcpTx) FindRefreshOwner(ctx context.Context, refreshHash []byte) (string, string, error) {
	if err := t.op("FindRefreshOwner"); err != nil {
		return "", "", err
	}
	refresh, ok := t.s.refresh[string(refreshHash)]
	if !ok {
		return "", "", ErrNotFound
	}
	return t.s.grants[refresh.GrantID].UserID, refresh.GrantID, nil
}

func (t *mcpTx) LockGrant(ctx context.Context, grantID string) (MCPGrantRecord, error) {
	if err := t.op("LockGrant"); err != nil {
		return MCPGrantRecord{}, err
	}
	grant, ok := t.s.grants[grantID]
	if !ok {
		return MCPGrantRecord{}, ErrNotFound
	}
	return grant, nil
}

func (t *mcpTx) LockRefreshToken(ctx context.Context, refreshHash []byte) (MCPRefreshRecord, error) {
	if err := t.op("LockRefreshToken"); err != nil {
		return MCPRefreshRecord{}, err
	}
	refresh, ok := t.s.refresh[string(refreshHash)]
	if !ok {
		return MCPRefreshRecord{}, ErrNotFound
	}
	return refresh, nil
}

func (t *mcpTx) ConsumeRefreshToken(ctx context.Context, refreshHash []byte, now time.Time) error {
	if err := t.op("ConsumeRefreshToken"); err != nil {
		return err
	}
	refresh, ok := t.s.refresh[string(refreshHash)]
	if !ok {
		return ErrNotFound
	}
	refresh.Consumed = true
	t.s.refresh[string(refreshHash)] = refresh
	return nil
}

// RevokeGrant mirrors the production contract: a live grant is revoked with
// its cause, its codes and tokens are deleted, and an already revoked grant
// changes nothing.
func (t *mcpTx) RevokeGrant(ctx context.Context, grantID string, now time.Time, revocation MCPRevocation) error {
	if err := t.op("RevokeGrant"); err != nil {
		return err
	}
	if !revocation.Reason.Valid() {
		return fmt.Errorf("invalid revoke reason %q", revocation.Reason)
	}
	grant, ok := t.s.grants[grantID]
	if !ok {
		return ErrNotFound
	}
	if grant.Revoked {
		return nil
	}
	grant.Revoked = true
	t.s.grants[grantID] = grant
	t.s.revocations[grantID] = revocation
	for hash, code := range t.s.codes {
		if code.Used && code.IssuedGrantID == grantID {
			delete(t.s.codes, hash)
		}
	}
	for hash, issue := range t.s.access {
		if issue.GrantID == grantID {
			delete(t.s.access, hash)
		}
	}
	for hash, refresh := range t.s.refresh {
		if refresh.GrantID == grantID {
			delete(t.s.refresh, hash)
		}
	}
	return nil
}

// CreateAuthorizationRequest mirrors the foreign key: an unknown client is
// ErrMCPInvalidClient.
func (t *mcpTx) CreateAuthorizationRequest(ctx context.Context, request MCPAuthorizationRequestRecord) error {
	if err := t.op("CreateAuthorizationRequest"); err != nil {
		return err
	}
	if _, ok := t.f.clients[request.ClientID]; !ok {
		return ErrMCPInvalidClient
	}
	t.s.requests[string(request.RequestHash)] = request
	return nil
}

// pendingRequest returns the unexpired consent with this handle digest bound
// to this binding digest, or ErrNotFound. A wrong binding matches nothing.
func (t *mcpTx) pendingRequest(requestHash, bindingHash []byte, now time.Time) (MCPAuthorizationRequestRecord, error) {
	rec, ok := t.s.requests[string(requestHash)]
	if !ok || !slices.Equal(rec.BindingHash, bindingHash) || !rec.ExpiresAt.After(now) {
		return MCPAuthorizationRequestRecord{}, ErrNotFound
	}
	rec.ClientName = t.f.clients[rec.ClientID].Name
	return rec, nil
}

func (t *mcpTx) FindAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (MCPAuthorizationRequestRecord, error) {
	if err := t.op("FindAuthorizationRequest"); err != nil {
		return MCPAuthorizationRequestRecord{}, err
	}
	return t.pendingRequest(requestHash, bindingHash, now)
}

func (t *mcpTx) ConsumeAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (MCPAuthorizationRequestRecord, error) {
	if err := t.op("ConsumeAuthorizationRequest"); err != nil {
		return MCPAuthorizationRequestRecord{}, err
	}
	rec, err := t.pendingRequest(requestHash, bindingHash, now)
	if err != nil {
		return MCPAuthorizationRequestRecord{}, err
	}
	delete(t.s.requests, string(requestHash))
	return rec, nil
}

// FindTokenGrantOwner resolves an access or refresh digest, consumed refresh
// digests included, to the account and grant. It does not check expiry.
func (t *mcpTx) FindTokenGrantOwner(ctx context.Context, tokenHash []byte) (string, string, error) {
	if err := t.op("FindTokenGrantOwner"); err != nil {
		return "", "", err
	}
	grantID := ""
	if issue, ok := t.s.access[string(tokenHash)]; ok {
		grantID = issue.GrantID
	} else if refresh, ok := t.s.refresh[string(tokenHash)]; ok {
		grantID = refresh.GrantID
	} else {
		return "", "", ErrNotFound
	}
	return t.s.grants[grantID].UserID, grantID, nil
}

// mcpFixture is an enabled service over a fresh fake at mcpTestStart.
type mcpFixture struct {
	t   *testing.T
	f   *mcpFake
	svc *MCPService
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	f := newMCPFake(mcpTestStart)
	svc, err := NewMCPService(f, MCPOptions{Enabled: true, Resource: mcpTestResource}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	return &mcpFixture{t: t, f: f, svc: svc}
}

func mcpValidAuthorization() MCPAuthorization {
	return MCPAuthorization{
		Principal:           Principal{UserID: mcpTestUser, Login: "owner"},
		SessionToken:        mcpTestSession,
		ClientID:            mcpTestClient,
		RedirectURI:         mcpTestRedirect,
		CodeChallenge:       mcpTestChallenge,
		CodeChallengeMethod: "S256",
		Resource:            mcpTestResource,
		Scopes:              []string{MCPScopeRead},
	}
}

func mcpValidExchange(code string) MCPCodeExchange {
	return MCPCodeExchange{
		Code:         code,
		CodeVerifier: mcpTestVerifier,
		ClientID:     mcpTestClient,
		RedirectURI:  mcpTestRedirect,
		Resource:     mcpTestResource,
	}
}

func mcpDigest(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func (fx *mcpFixture) issueCode(in MCPAuthorization) string {
	fx.t.Helper()
	issued, err := fx.svc.IssueAuthorizationCode(context.Background(), in)
	if err != nil {
		fx.t.Fatalf("IssueAuthorizationCode: %v", err)
	}
	return issued.Code
}

// exchangeOK issues a default code and redeems it, returning both.
func (fx *mcpFixture) exchangeOK() (string, MCPTokens) {
	fx.t.Helper()
	code := fx.issueCode(mcpValidAuthorization())
	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
	if err != nil {
		fx.t.Fatalf("ExchangeCode: %v", err)
	}
	return code, tokens
}

// liveGrantIDs returns the ids of unrevoked grants in the committed state.
func (f *mcpFake) liveGrantIDs() []string {
	var ids []string
	for id, grant := range f.state.grants {
		if !grant.Revoked {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestMCPDisabledRefusesWithoutTouchingRepository(t *testing.T) {
	f := newMCPFake(mcpTestStart)
	svc, err := NewMCPService(f, MCPOptions{Enabled: false}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	secret := strings.Repeat("A", 43)
	tests := []struct {
		name string
		call func() error
	}{
		{"IssueAuthorizationCode", func() error {
			_, e := svc.IssueAuthorizationCode(ctx, mcpValidAuthorization())
			return e
		}},
		{"ExchangeCode", func() error {
			_, e := svc.ExchangeCode(ctx, mcpValidExchange(secret))
			return e
		}},
		{"Refresh", func() error {
			_, e := svc.Refresh(ctx, MCPRefresh{RefreshToken: secret, ClientID: mcpTestClient, Resource: mcpTestResource})
			return e
		}},
		{"VerifyAccessToken", func() error {
			_, e := svc.VerifyAccessToken(ctx, secret)
			return e
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.calls = nil
			if err := tt.call(); !errors.Is(err, ErrMCPDisabled) {
				t.Fatalf("err = %v, want ErrMCPDisabled", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("repository touched: %v", f.calls)
			}
		})
	}
}

func TestNewMCPServiceValidatesConfiguration(t *testing.T) {
	f := newMCPFake(mcpTestStart)
	tests := []struct {
		name    string
		repo    MCPRepository
		opts    MCPOptions
		wantErr bool
	}{
		{"nil repository", nil, MCPOptions{}, true},
		{"enabled without resource", f, MCPOptions{Enabled: true}, true},
		{"disabled without resource", f, MCPOptions{}, false},
		{"enabled with resource", f, MCPOptions{Enabled: true, Resource: mcpTestResource}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMCPService(tt.repo, tt.opts, f.clock)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMCPIssueAuthorizationCodeRefusals(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*mcpFixture)
		mutate func(*MCPAuthorization)
		want   error
	}{
		{name: "missing principal", mutate: func(in *MCPAuthorization) { in.Principal.UserID = "" }, want: ErrUnauthenticated},
		{name: "missing session token", mutate: func(in *MCPAuthorization) { in.SessionToken = "" }, want: ErrUnauthenticated},
		{name: "plain method", mutate: func(in *MCPAuthorization) { in.CodeChallengeMethod = "plain" }, want: ErrMCPInvalidRequest},
		{name: "empty method", mutate: func(in *MCPAuthorization) { in.CodeChallengeMethod = "" }, want: ErrMCPInvalidRequest},
		{name: "challenge 42 chars", mutate: func(in *MCPAuthorization) { in.CodeChallenge = mcpTestChallenge[:42] }, want: ErrMCPInvalidRequest},
		{name: "challenge 44 chars", mutate: func(in *MCPAuthorization) { in.CodeChallenge = mcpTestChallenge + "A" }, want: ErrMCPInvalidRequest},
		{name: "challenge bad alphabet plus", mutate: func(in *MCPAuthorization) { in.CodeChallenge = "+" + mcpTestChallenge[1:] }, want: ErrMCPInvalidRequest},
		{name: "challenge padded", mutate: func(in *MCPAuthorization) { in.CodeChallenge = mcpTestChallenge[:42] + "=" }, want: ErrMCPInvalidRequest},
		{name: "resource mismatch", mutate: func(in *MCPAuthorization) { in.Resource = "https://evil.test/mcp" }, want: ErrMCPInvalidRequest},
		{name: "resource empty", mutate: func(in *MCPAuthorization) { in.Resource = "" }, want: ErrMCPInvalidRequest},
		{name: "unknown scope", mutate: func(in *MCPAuthorization) { in.Scopes = []string{MCPScopeRead, "tendo:admin"} }, want: ErrMCPInvalidRequest},
		{name: "nil scopes", mutate: func(in *MCPAuthorization) { in.Scopes = nil }, want: ErrMCPInvalidRequest},
		{name: "empty scopes", mutate: func(in *MCPAuthorization) { in.Scopes = []string{} }, want: ErrMCPInvalidRequest},
		{name: "unknown client", mutate: func(in *MCPAuthorization) { in.ClientID = "nobody" }, want: ErrMCPInvalidClient},
		{name: "redirect trailing slash", mutate: func(in *MCPAuthorization) { in.RedirectURI = mcpTestRedirect + "/" }, want: ErrMCPInvalidRequest},
		{name: "redirect other host", mutate: func(in *MCPAuthorization) { in.RedirectURI = "https://evil.test/callback" }, want: ErrMCPInvalidRequest},
		{name: "deleted account", setup: func(fx *mcpFixture) { fx.f.accounts[mcpTestUser] = false }, want: ErrUnauthenticated},
		{name: "session not active", setup: func(fx *mcpFixture) { fx.f.sessionActive = false }, want: ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			if tt.setup != nil {
				tt.setup(fx)
			}
			in := mcpValidAuthorization()
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			_, err := fx.svc.IssueAuthorizationCode(context.Background(), in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if len(fx.f.state.codes) != 0 {
				t.Fatalf("refused issuance stored %d codes", len(fx.f.state.codes))
			}
		})
	}
}

func TestMCPIssueAuthorizationCodeStoresDigestOnly(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = []string{MCPScopeWrite, MCPScopeRead, MCPScopeWrite}
	issued, err := fx.svc.IssueAuthorizationCode(context.Background(), in)
	if err != nil {
		t.Fatalf("IssueAuthorizationCode: %v", err)
	}
	if len(issued.Code) != 43 || !mcpSecretPattern.MatchString(issued.Code) {
		t.Fatalf("code %q is not 43 base64url characters", issued.Code)
	}
	if want := mcpTestStart.Add(MCPCodeLifetime); !issued.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", issued.ExpiresAt, want)
	}
	if len(fx.f.state.codes) != 1 {
		t.Fatalf("stored %d codes, want 1", len(fx.f.state.codes))
	}
	rec, ok := fx.f.state.codes[string(mcpDigest(issued.Code))]
	if !ok {
		t.Fatal("stored record is not keyed by sha256(code)")
	}
	if _, plain := fx.f.state.codes[issued.Code]; plain {
		t.Fatal("plaintext code used as a storage key")
	}
	if !slices.Equal(rec.CodeHash, mcpDigest(issued.Code)) {
		t.Fatal("CodeHash is not sha256(code)")
	}
	if !slices.Equal(rec.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
		t.Fatalf("scopes = %v, want deduplicated and sorted", rec.Scopes)
	}
	if rec.UserID != mcpTestUser || rec.ClientID != mcpTestClient || rec.RedirectURI != mcpTestRedirect ||
		rec.CodeChallenge != mcpTestChallenge || rec.Resource != mcpTestResource || rec.Used || rec.IssuedGrantID != "" {
		t.Fatalf("unexpected stored record: %+v", rec)
	}
	if !rec.ExpiresAt.Equal(issued.ExpiresAt) || !rec.CreatedAt.Equal(mcpTestStart) {
		t.Fatalf("stored times = %v..%v", rec.CreatedAt, rec.ExpiresAt)
	}
	want := []string{"WithMCPTransaction", "LockLiveAccount", "ActiveSessionExists", "FindClient", "CreateAuthorizationCode"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, want)
	}
}

func TestMCPExchangeCodeCreatesGrantAndTokens(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = []string{MCPScopeWrite, MCPScopeRead}
	code := fx.issueCode(in)
	fx.f.calls = nil

	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	want := []string{"WithMCPTransaction", "FindCodeOwner", "LockLiveAccount", "LockAuthorizationCode",
		"CreateGrant", "MarkAuthorizationCodeUsed", "CreateTokens"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, want)
	}

	if len(fx.f.state.grants) != 1 {
		t.Fatalf("grants = %d, want 1", len(fx.f.state.grants))
	}
	var grantID string
	var grant MCPGrantRecord
	for id, g := range fx.f.state.grants {
		grantID, grant = id, g
	}
	if grant.UserID != mcpTestUser || grant.ClientID != mcpTestClient || grant.Resource != mcpTestResource {
		t.Fatalf("grant binding = %+v", grant)
	}
	if !slices.Equal(grant.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
		t.Fatalf("grant scopes = %v", grant.Scopes)
	}
	if want := mcpTestStart.Add(MCPGrantLifetime); !grant.ExpiresAt.Equal(want) {
		t.Fatalf("grant expires %v, want %v", grant.ExpiresAt, want)
	}
	if grant.Revoked {
		t.Fatal("new grant is revoked")
	}

	codeRec := fx.f.state.codes[string(mcpDigest(code))]
	if !codeRec.Used || codeRec.IssuedGrantID != grantID {
		t.Fatalf("code = used:%v grant:%q, want used with %q", codeRec.Used, codeRec.IssuedGrantID, grantID)
	}

	if want := mcpTestStart.Add(MCPAccessTokenLifetime); !tokens.AccessExpiresAt.Equal(want) {
		t.Fatalf("access expires %v, want %v", tokens.AccessExpiresAt, want)
	}
	if !slices.Equal(tokens.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
		t.Fatalf("token scopes = %v", tokens.Scopes)
	}
	if !mcpSecretPattern.MatchString(tokens.AccessToken) || !mcpSecretPattern.MatchString(tokens.RefreshToken) {
		t.Fatal("token plaintexts are not 43-character secrets")
	}
	if tokens.AccessToken == tokens.RefreshToken {
		t.Fatal("access and refresh tokens are identical")
	}

	issue, ok := fx.f.state.access[string(mcpDigest(tokens.AccessToken))]
	if !ok {
		t.Fatal("access token not stored under sha256(token)")
	}
	if _, plain := fx.f.state.access[tokens.AccessToken]; plain {
		t.Fatal("plaintext access token stored")
	}
	if issue.GrantID != grantID || !issue.AccessExpiresAt.Equal(mcpTestStart.Add(MCPAccessTokenLifetime)) {
		t.Fatalf("access row = %+v", issue)
	}
	refresh, ok := fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))]
	if !ok {
		t.Fatal("refresh token not stored under sha256(token)")
	}
	if _, plain := fx.f.state.refresh[tokens.RefreshToken]; plain {
		t.Fatal("plaintext refresh token stored")
	}
	if refresh.GrantID != grantID || !refresh.ExpiresAt.Equal(mcpTestStart.Add(MCPRefreshIdleLifetime)) || refresh.Consumed {
		t.Fatalf("refresh row = %+v", refresh)
	}
}

func TestMCPExchangeCodeRefusalsCreateNothing(t *testing.T) {
	const otherVerifier = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	tests := []struct {
		name   string
		mutate func(*MCPCodeExchange)
		clock  time.Duration // advance applied after issuance
		setup  func(*mcpFixture)
		code   string // replaces the issued code when non-empty
	}{
		{name: "wrong verifier", mutate: func(in *MCPCodeExchange) { in.CodeVerifier = otherVerifier }},
		{name: "wrong client", mutate: func(in *MCPCodeExchange) { in.ClientID = "other-client" }},
		{name: "wrong redirect", mutate: func(in *MCPCodeExchange) { in.RedirectURI = mcpTestRedirect + "/" }},
		{name: "wrong resource", mutate: func(in *MCPCodeExchange) { in.Resource = "https://evil.test/mcp" }},
		{name: "expired one microsecond late", clock: MCPCodeLifetime + time.Microsecond},
		{name: "expired exactly at expiry", clock: MCPCodeLifetime},
		{name: "unknown code", code: strings.Repeat("B", 43)},
		{name: "code 42 chars", code: strings.Repeat("B", 42)},
		{name: "verifier 42 chars", mutate: func(in *MCPCodeExchange) { in.CodeVerifier = mcpTestVerifier[:42] }},
		{name: "verifier 129 chars", mutate: func(in *MCPCodeExchange) {
			in.CodeVerifier = mcpTestVerifier + strings.Repeat("a", 86)
		}},
		{name: "verifier bad alphabet", mutate: func(in *MCPCodeExchange) {
			in.CodeVerifier = mcpTestVerifier[:42] + "!"
		}},
		{name: "deleted owner", setup: func(fx *mcpFixture) { fx.f.accounts[mcpTestUser] = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			code := fx.issueCode(mcpValidAuthorization())
			if tt.setup != nil {
				tt.setup(fx)
			}
			fx.f.now = mcpTestStart.Add(tt.clock)
			in := mcpValidExchange(code)
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			if tt.code != "" {
				in.Code = tt.code
			}
			_, err := fx.svc.ExchangeCode(context.Background(), in)
			if !errors.Is(err, ErrMCPInvalidGrant) {
				t.Fatalf("err = %v, want ErrMCPInvalidGrant", err)
			}
			if len(fx.f.state.grants) != 0 || len(fx.f.state.access) != 0 || len(fx.f.state.refresh) != 0 {
				t.Fatalf("refusal created grants=%d access=%d refresh=%d",
					len(fx.f.state.grants), len(fx.f.state.access), len(fx.f.state.refresh))
			}
			for hash, c := range fx.f.state.codes {
				if c.Used {
					t.Fatalf("refusal marked code %x used", hash)
				}
			}
		})
	}
}

func TestMCPExchangeCodeReuseRevokesGrantCommitted(t *testing.T) {
	fx := newMCPFixture(t)
	code, tokens := fx.exchangeOK()
	var grantID string
	for id := range fx.f.state.grants {
		grantID = id
	}

	_, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
	if !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("replayed exchange err = %v, want ErrMCPInvalidGrant", err)
	}

	// The revocation must be in the committed state even though the request failed.
	got, ok := fx.f.state.revocations[grantID]
	if !ok {
		t.Fatal("code reuse did not commit a revocation")
	}
	if got.Reason != MCPRevokeCodeReuse || got.ActorUserID != "" {
		t.Fatalf("revocation = %+v, want code_reuse with empty actor", got)
	}
	if !fx.f.state.grants[grantID].Revoked {
		t.Fatal("grant not marked revoked in committed state")
	}
	if len(fx.f.state.access) != 0 || len(fx.f.state.refresh) != 0 {
		t.Fatalf("tokens survived revocation: access=%d refresh=%d", len(fx.f.state.access), len(fx.f.state.refresh))
	}

	if _, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken); !errors.Is(err, ErrMCPInvalidToken) {
		t.Fatalf("access token after code reuse: err = %v, want ErrMCPInvalidToken", err)
	}
	if _, err := fx.svc.Refresh(context.Background(), MCPRefresh{RefreshToken: tokens.RefreshToken, ClientID: mcpTestClient, Resource: mcpTestResource}); !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("refresh after code reuse: err = %v, want ErrMCPInvalidGrant", err)
	}
}

func mcpRefreshOf(tokens MCPTokens) MCPRefresh {
	return MCPRefresh{RefreshToken: tokens.RefreshToken, ClientID: mcpTestClient, Resource: mcpTestResource}
}

// onlyGrantID returns the sole grant in the committed state.
func (f *mcpFake) onlyGrantID(t *testing.T) string {
	t.Helper()
	if len(f.state.grants) != 1 {
		t.Fatalf("grants = %d, want 1", len(f.state.grants))
	}
	for id := range f.state.grants {
		return id
	}
	return ""
}

func TestMCPRefreshRotatesAndReplayRevokesCommitted(t *testing.T) {
	fx := newMCPFixture(t)
	_, first := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	oldRefresh := mcpDigest(first.RefreshToken)

	fx.f.calls = nil
	second, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(first))
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	want := []string{"WithMCPTransaction", "FindRefreshOwner", "LockLiveAccount", "LockGrant",
		"LockRefreshToken", "ConsumeRefreshToken", "CreateTokens"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, want)
	}
	if !slices.Equal(second.Scopes, []string{MCPScopeRead}) {
		t.Fatalf("scopes = %v, want the grant's scopes", second.Scopes)
	}
	if !fx.f.state.refresh[string(oldRefresh)].Consumed {
		t.Fatal("old refresh token not consumed")
	}
	newRow, ok := fx.f.state.refresh[string(mcpDigest(second.RefreshToken))]
	if !ok || newRow.GrantID != grantID || newRow.Consumed {
		t.Fatalf("new refresh row = %+v, ok=%v", newRow, ok)
	}

	// Replaying the consumed token fails and revokes the whole grant, committed.
	_, err = fx.svc.Refresh(context.Background(), mcpRefreshOf(first))
	if !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("replay err = %v, want ErrMCPInvalidGrant", err)
	}
	rev, ok := fx.f.state.revocations[grantID]
	if !ok || rev.Reason != MCPRevokeReplay || rev.ActorUserID != "" {
		t.Fatalf("revocation = %+v ok=%v, want replay with empty actor", rev, ok)
	}
	if !fx.f.state.grants[grantID].Revoked {
		t.Fatal("grant not revoked in committed state")
	}

	// The token minted before the replay died with the grant.
	_, err = fx.svc.Refresh(context.Background(), mcpRefreshOf(second))
	if !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("post-replay refresh err = %v, want ErrMCPInvalidGrant", err)
	}
	if _, err := fx.svc.VerifyAccessToken(context.Background(), second.AccessToken); !errors.Is(err, ErrMCPInvalidToken) {
		t.Fatalf("post-replay access err = %v, want ErrMCPInvalidToken", err)
	}
}

func TestMCPRefreshRefusalsDoNotConsume(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*mcpFixture, string)
		mutate func(*MCPRefresh)
	}{
		{name: "wrong client", mutate: func(in *MCPRefresh) { in.ClientID = "other-client" }},
		{name: "wrong resource", mutate: func(in *MCPRefresh) { in.Resource = "https://evil.test/mcp" }},
		{name: "expired refresh token", setup: func(fx *mcpFixture, _ string) {
			fx.f.now = mcpTestStart.Add(MCPRefreshIdleLifetime + time.Second)
		}},
		{name: "expired grant", setup: func(fx *mcpFixture, grantID string) {
			g := fx.f.state.grants[grantID]
			g.ExpiresAt = mcpTestStart.Add(-time.Second)
			fx.f.state.grants[grantID] = g
		}},
		{name: "revoked grant", setup: func(fx *mcpFixture, grantID string) {
			g := fx.f.state.grants[grantID]
			g.Revoked = true
			fx.f.state.grants[grantID] = g
		}},
		{name: "deleted owner", setup: func(fx *mcpFixture, _ string) { fx.f.accounts[mcpTestUser] = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			_, tokens := fx.exchangeOK()
			grantID := fx.f.onlyGrantID(t)
			if tt.setup != nil {
				tt.setup(fx, grantID)
			}
			in := mcpRefreshOf(tokens)
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			_, err := fx.svc.Refresh(context.Background(), in)
			if !errors.Is(err, ErrMCPInvalidGrant) {
				t.Fatalf("err = %v, want ErrMCPInvalidGrant", err)
			}
			row := fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))]
			if row.Consumed {
				t.Fatal("refused refresh consumed the token")
			}
			if _, revoked := fx.f.state.revocations[grantID]; revoked {
				t.Fatal("refusal revoked the grant")
			}
		})
	}
}

func TestMCPRefreshCapsTokenExpiryAtGrantExpiry(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	grantEnd := mcpTestStart.Add(5 * time.Minute)
	g := fx.f.state.grants[grantID]
	g.ExpiresAt = grantEnd
	fx.f.state.grants[grantID] = g

	next, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens))
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !next.AccessExpiresAt.Equal(grantEnd) {
		t.Fatalf("access expires %v, want grant expiry %v", next.AccessExpiresAt, grantEnd)
	}
	access := fx.f.state.access[string(mcpDigest(next.AccessToken))]
	if !access.AccessExpiresAt.Equal(grantEnd) {
		t.Fatalf("stored access expiry %v, want %v", access.AccessExpiresAt, grantEnd)
	}
	refresh := fx.f.state.refresh[string(mcpDigest(next.RefreshToken))]
	if !refresh.ExpiresAt.Equal(grantEnd) {
		t.Fatalf("stored refresh expiry %v, want %v", refresh.ExpiresAt, grantEnd)
	}
}

func TestMCPVerifyAccessTokenMalformedNeverReachesRepository(t *testing.T) {
	fx := newMCPFixture(t)
	for _, token := range []string{"", "short", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("+", 43)} {
		fx.f.calls = nil
		if _, err := fx.svc.VerifyAccessToken(context.Background(), token); !errors.Is(err, ErrMCPInvalidToken) {
			t.Fatalf("token %q: err = %v, want ErrMCPInvalidToken", token, err)
		}
		if len(fx.f.calls) != 0 {
			t.Fatalf("token %q touched repository: %v", token, fx.f.calls)
		}
	}
}

func TestMCPVerifyAccessTokenOutcomes(t *testing.T) {
	t.Run("unknown token is invalid", func(t *testing.T) {
		fx := newMCPFixture(t)
		_, err := fx.svc.VerifyAccessToken(context.Background(), strings.Repeat("A", 43))
		if !errors.Is(err, ErrMCPInvalidToken) {
			t.Fatalf("err = %v, want ErrMCPInvalidToken", err)
		}
	})

	t.Run("resource mismatch is invalid", func(t *testing.T) {
		fx := newMCPFixture(t)
		_, tokens := fx.exchangeOK()
		grantID := fx.f.onlyGrantID(t)
		g := fx.f.state.grants[grantID]
		g.Resource = "https://evil.test/mcp"
		fx.f.state.grants[grantID] = g
		_, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken)
		if !errors.Is(err, ErrMCPInvalidToken) {
			t.Fatalf("err = %v, want ErrMCPInvalidToken", err)
		}
	})

	t.Run("success returns the grant binding", func(t *testing.T) {
		fx := newMCPFixture(t)
		in := mcpValidAuthorization()
		in.Scopes = []string{MCPScopeWrite, MCPScopeRead}
		code := fx.issueCode(in)
		tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
		if err != nil {
			t.Fatalf("ExchangeCode: %v", err)
		}
		grantID := fx.f.onlyGrantID(t)
		access, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken)
		if err != nil {
			t.Fatalf("VerifyAccessToken: %v", err)
		}
		want := MCPAccess{GrantID: grantID, UserID: mcpTestUser, ClientID: mcpTestClient,
			Scopes: []string{MCPScopeRead, MCPScopeWrite}}
		if access.GrantID != want.GrantID || access.UserID != want.UserID ||
			access.ClientID != want.ClientID || !slices.Equal(access.Scopes, want.Scopes) {
			t.Fatalf("access = %+v, want %+v", access, want)
		}
	})

	t.Run("expired access token is invalid", func(t *testing.T) {
		fx := newMCPFixture(t)
		_, tokens := fx.exchangeOK()
		fx.f.now = mcpTestStart.Add(MCPAccessTokenLifetime)
		_, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken)
		if !errors.Is(err, ErrMCPInvalidToken) {
			t.Fatalf("err = %v, want ErrMCPInvalidToken", err)
		}
	})

	t.Run("infrastructure error propagates", func(t *testing.T) {
		fx := newMCPFixture(t)
		fx.f.findActiveErr = errMCPInjected
		_, err := fx.svc.VerifyAccessToken(context.Background(), strings.Repeat("A", 43))
		if !errors.Is(err, errMCPInjected) {
			t.Fatalf("err = %v, want the injected infrastructure error", err)
		}
		if errors.Is(err, ErrMCPInvalidToken) {
			t.Fatal("infrastructure error mapped to ErrMCPInvalidToken")
		}
	})
}

func TestMCPPKCES256Matches(t *testing.T) {
	if !pkceS256Matches(mcpTestVerifier, mcpTestChallenge) {
		t.Fatal("RFC 7636 Appendix B vector does not match")
	}
	if pkceS256Matches(mcpTestVerifier[:42]+"A", mcpTestChallenge) {
		t.Fatal("altered verifier matched")
	}
	if pkceS256Matches(mcpTestVerifier, mcpTestChallenge[:42]+"A") {
		t.Fatal("altered challenge matched")
	}
}

func TestMCPRevokeReasonValid(t *testing.T) {
	accepted := []MCPRevokeReason{"user", "password_change", "admin_reset", "cli_reset",
		"account_deletion", "replay", "code_reuse", "client"}
	for _, r := range accepted {
		if !r.Valid() {
			t.Errorf("%q rejected, want accepted", r)
		}
	}
	for _, r := range []MCPRevokeReason{"", "User", "code-reuse", "expired", "revoked", " replay", "other"} {
		if r.Valid() {
			t.Errorf("%q accepted, want rejected", r)
		}
	}
}

// TestMCPRandomOperationsKeepSingleUse drives random exchange, replay and
// refresh sequences against the fake and checks the invariants after each step.
func TestMCPRandomOperationsKeepSingleUse(t *testing.T) {
	const sequences = 50
	const stepsPerSequence = 40
	for seq := 0; seq < sequences; seq++ {
		rng := rand.New(rand.NewPCG(uint64(seq+1), 0x6d6370))
		fx := newMCPFixture(t)
		ctx := context.Background()
		var codes []string
		var refreshes []MCPRefresh
		exchanges := map[string]int{}
		refreshAccepted := map[string]int{}
		codeGrant := map[string]string{}
		replayed := map[string]bool{}

		for step := 0; step < stepsPerSequence; step++ {
			switch rng.IntN(4) {
			case 0: // issue and exchange a fresh code
				in := mcpValidAuthorization()
				in.Scopes = []string{[]string{MCPScopeRead, MCPScopeWrite}[rng.IntN(2)]}
				issued, err := fx.svc.IssueAuthorizationCode(ctx, in)
				if err != nil {
					t.Fatalf("seq %d step %d: issue: %v", seq, step, err)
				}
				codes = append(codes, issued.Code)
				tokens, err := fx.svc.ExchangeCode(ctx, mcpValidExchange(issued.Code))
				if err == nil {
					exchanges[issued.Code]++
					codeGrant[issued.Code] = fx.f.state.codes[string(mcpDigest(issued.Code))].IssuedGrantID
					refreshes = append(refreshes, mcpRefreshOf(tokens))
				} else if !errors.Is(err, ErrMCPInvalidGrant) {
					t.Fatalf("seq %d step %d: fresh exchange: %v", seq, step, err)
				}
			case 1: // replay an earlier code
				if len(codes) == 0 {
					continue
				}
				code := codes[rng.IntN(len(codes))]
				_, err := fx.svc.ExchangeCode(ctx, mcpValidExchange(code))
				if err == nil {
					// The first redemption already happened; a second success is the bug.
					t.Fatalf("seq %d step %d: code redeemed twice", seq, step)
				} else if !errors.Is(err, ErrMCPInvalidGrant) {
					t.Fatalf("seq %d step %d: replay exchange: %v", seq, step, err)
				}
				replayed[code] = true
			case 2, 3: // refresh a random earlier refresh token, consumed or not
				if len(refreshes) == 0 {
					continue
				}
				in := refreshes[rng.IntN(len(refreshes))]
				tokens, err := fx.svc.Refresh(ctx, in)
				if err == nil {
					refreshAccepted[in.RefreshToken]++
					refreshes = append(refreshes, mcpRefreshOf(tokens))
				} else if !errors.Is(err, ErrMCPInvalidGrant) {
					t.Fatalf("seq %d step %d: refresh: %v", seq, step, err)
				}
			}

			// Invariants, checked after every step.
			for code, n := range exchanges {
				if n > 1 {
					t.Fatalf("seq %d: code redeemed %d times", seq, n)
				}
				grantID := codeGrant[code]
				if grantID == "" {
					continue
				}
				if replayed[code] && !fx.f.state.grants[grantID].Revoked {
					t.Fatalf("seq %d: replayed code %s left grant %s live", seq, code, grantID)
				}
			}
			for _, n := range refreshAccepted {
				if n > 1 {
					t.Fatalf("seq %d: refresh token accepted %d times", seq, n)
				}
			}
			if live := len(fx.f.liveGrantIDs()); live > len(exchanges) {
				t.Fatalf("seq %d: %d live grants from %d successful exchanges", seq, live, len(exchanges))
			}
		}
	}
}

// TestMCPInfrastructureFailureRollsBackEverything proves that a storage error
// after the grant is created never leaves a half-issued grant or a used code:
// the client can retry the same code.
func TestMCPInfrastructureFailureRollsBackEverything(t *testing.T) {
	for _, step := range []string{"CreateGrant", "MarkAuthorizationCodeUsed", "CreateTokens"} {
		t.Run(step, func(t *testing.T) {
			fx := newMCPFixture(t)
			code := fx.issueCode(mcpValidAuthorization())
			fx.f.failOn = step
			if _, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code)); !errors.Is(err, errMCPInjected) {
				t.Fatalf("err = %v, want the injected failure", err)
			}
			if len(fx.f.state.grants) != 0 || len(fx.f.state.access) != 0 || len(fx.f.state.refresh) != 0 {
				t.Fatalf("half-issued state committed: %d grants, %d access, %d refresh",
					len(fx.f.state.grants), len(fx.f.state.access), len(fx.f.state.refresh))
			}
			if fx.f.state.codes[string(mcpDigest(code))].Used {
				t.Fatal("code marked used by a rolled-back exchange")
			}
			fx.f.failOn = ""
			if _, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code)); err != nil {
				t.Fatalf("retry after rollback: %v", err)
			}
		})
	}
}

// TestMCPRefreshInfrastructureErrorOnRevokedGrantRollsBack proves a storage
// failure while locking the refresh token is reported as that failure, not as
// a clean invalid_grant, even when the grant is already revoked.
func TestMCPRefreshInfrastructureErrorOnRevokedGrantRollsBack(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	g := fx.f.state.grants[grantID]
	g.Revoked = true
	fx.f.state.grants[grantID] = g
	fx.f.failOn = "LockRefreshToken"
	_, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(tokens))
	if !errors.Is(err, errMCPInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
	if fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))].Consumed {
		t.Fatal("failed refresh consumed the token")
	}
}
