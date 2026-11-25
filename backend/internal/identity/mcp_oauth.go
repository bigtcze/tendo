package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"time"
)

// MCP OAuth lifetimes (ADR 0023). The grant lifetime is absolute; a refresh
// token is rotated on every use and dies after MCPRefreshIdleLifetime unused.
const (
	MCPCodeLifetime        = 60 * time.Second
	MCPAccessTokenLifetime = 10 * time.Minute
	MCPRefreshIdleLifetime = 30 * 24 * time.Hour
	MCPGrantLifetime       = 90 * 24 * time.Hour
)

// The MCP scopes. A grant stores the consented set; every use intersects it
// with the installation, account and user policy (#457, mcp_policy.go).
const (
	MCPScopeRead  = "tendo:read"
	MCPScopeWrite = "tendo:write"
)

var (
	// ErrMCPDisabled refuses every issuance and verification while
	// MCP is disabled, independently of HTTP routing.
	ErrMCPDisabled = errors.New("mcp disabled")
	// ErrMCPInvalidRequest is a malformed authorization request: unsupported
	// PKCE method, malformed challenge, unknown scope, wrong resource or a
	// redirect URI the client never registered.
	ErrMCPInvalidRequest = errors.New("mcp invalid request")
	// ErrMCPInvalidClient is an unknown client.
	ErrMCPInvalidClient = errors.New("mcp invalid client")
	// ErrMCPInvalidGrant is the single OAuth invalid_grant outcome: unknown,
	// expired, used or mismatched code or refresh token, or a failed PKCE proof.
	// It never says which.
	ErrMCPInvalidGrant = errors.New("mcp invalid grant")
	// ErrMCPInvalidToken is any access token that does not authorize a request.
	ErrMCPInvalidToken = errors.New("mcp invalid token")
)

var (
	mcpSecretPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	mcpCodeVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// MCPOptions is the installation's MCP configuration. Resource is the
// canonical protected resource URI (<public URL>/mcp); every code, grant and
// token is bound to it. Its host is the public origin host (ADR 0023).
type MCPOptions struct {
	Enabled  bool
	Resource string
	// DBTimeout bounds the per-request installation switch read (B1b2). Zero
	// leaves the caller's context as the only bound.
	DBTimeout time.Duration
	// InstallationSwitch replaces the repository's installation switch reader
	// when set. Production leaves it nil, so the gate reads the database; tests
	// use it to model an installation that is on or off without a database.
	InstallationSwitch MCPInstallationStore
}

// MCPClient is a registered OAuth public client.
type MCPClient struct {
	ID, Name     string
	RedirectURIs []string
}

// MCPCodeRecord is a stored authorization code. Only the digest is stored.
type MCPCodeRecord struct {
	CodeHash                                               []byte
	UserID, ClientID, RedirectURI, CodeChallenge, Resource string
	Scopes                                                 []string
	CreatedAt, ExpiresAt                                   time.Time
	Used                                                   bool
	IssuedGrantID                                          string
}

// MCPGrantRecord is a user's authorization of one client for one resource.
type MCPGrantRecord struct {
	ID, UserID, ClientID, Resource string
	Scopes                         []string
	CreatedAt, ExpiresAt           time.Time
	Revoked                        bool
}

// MCPRefreshRecord is a stored refresh token digest.
type MCPRefreshRecord struct {
	GrantID   string
	ExpiresAt time.Time
	Consumed  bool
}

// MCPTokenIssue is one access and refresh token pair for a grant.
type MCPTokenIssue struct {
	GrantID                                      string
	AccessHash, RefreshHash                      []byte
	CreatedAt, AccessExpiresAt, RefreshExpiresAt time.Time
}

// MCPTransaction is the transaction-scoped persistence surface. Every issuance
// path takes its locks in the ADR 0023 order: LockLiveAccount (the account
// FOR SHARE, then its local credential FOR SHARE), then authorization codes,
// then the grant, then token rows. Not-found lookups return ErrNotFound.
type MCPTransaction interface {
	LockLiveAccount(ctx context.Context, userID string) error
	ActiveSessionExists(ctx context.Context, userID string, tokenHash []byte, now time.Time) (bool, error)
	FindClient(ctx context.Context, clientID string) (MCPClient, error)
	CreateAuthorizationCode(ctx context.Context, code MCPCodeRecord) error
	// FindCodeOwner is the unlocked pre-read that names the account to lock.
	FindCodeOwner(ctx context.Context, codeHash []byte) (string, error)
	LockAuthorizationCode(ctx context.Context, codeHash []byte) (MCPCodeRecord, error)
	MarkAuthorizationCodeUsed(ctx context.Context, codeHash []byte, grantID string, now time.Time) error
	// CreateGrant inserts the grant and its audited creation event.
	CreateGrant(ctx context.Context, grant MCPGrantRecord) (string, error)
	CreateTokens(ctx context.Context, issue MCPTokenIssue) error
	// FindRefreshOwner is the unlocked pre-read that names the account and
	// grant to lock.
	FindRefreshOwner(ctx context.Context, refreshHash []byte) (userID, grantID string, err error)
	// FindGrantOwner is the unlocked pre-read for a user's own revocation: the
	// account that owns the grant. An unknown grant is ErrNotFound.
	FindGrantOwner(ctx context.Context, grantID string) (string, error)
	// LockGrant locks the grant's used codes, then the grant, FOR UPDATE.
	LockGrant(ctx context.Context, grantID string) (MCPGrantRecord, error)
	LockRefreshToken(ctx context.Context, refreshHash []byte) (MCPRefreshRecord, error)
	ConsumeRefreshToken(ctx context.Context, refreshHash []byte, now time.Time) error
	// RevokeGrant revokes a live grant with an audited cause and deletes its
	// codes and tokens. Revoking an already revoked grant changes nothing.
	RevokeGrant(ctx context.Context, grantID string, now time.Time, revocation MCPRevocation) error
	// ReadPolicy reads the installation and account policy without a lock,
	// after the account lock. Missing rows come back as not found flags.
	ReadPolicy(ctx context.Context, userID string) (MCPPolicyRecord, error)
	MCPAuthorizationTransaction
}

// MCPRepository opens MCP transactions at READ COMMITTED and verifies access
// tokens without a transaction.
type MCPRepository interface {
	WithMCPTransaction(ctx context.Context, work func(MCPTransaction) error) error
	// FindActiveAccessToken returns the live grant of an unexpired access token
	// whose grant is unrevoked and unexpired and whose account is not deleted,
	// with the account's policy read in the same statement.
	FindActiveAccessToken(ctx context.Context, tokenHash []byte, now time.Time) (MCPActiveAccess, error)
}

// MCPService is the hand-written OAuth 2.1 authorization server core for MCP
// public clients (ADR 0023). It has no HTTP surface.
type MCPService struct {
	repository MCPRepository
	clock      func() time.Time
	enabled    bool
	resource   string
	// publicHost is Tendo's own canonical host (the resource's host), refused as
	// a self-redirect target.
	publicHost string
	// clientStore is the registration store. It is the same repository value as
	// repository when the implementation provides both.
	clientStore MCPClientStore
	// sweeper runs the retention steps. Nil fails the sweep closed.
	sweeper MCPSweepStore
	// grantStore reads the signed-in account's connected apps. Nil fails the
	// list closed.
	grantStore MCPGrantStore
	// installation reads the installation switch for the gate (B1b2). Nil fails
	// an enabled gate closed.
	installation MCPInstallationStore
	// dbTimeout bounds the unlocked installation read; zero means no extra bound.
	dbTimeout time.Duration
}

// MCPClientStore is the registration persistence (RFC 7591). It is outside the
// issuance transaction: registration never holds issuance locks.
type MCPClientStore interface {
	// CreateClientWithinCap inserts the client only while fewer than cap clients
	// without any grant exist, in one statement. It reports whether a row was
	// inserted. The cap is soft under concurrency (ADR 0023).
	CreateClientWithinCap(ctx context.Context, client MCPClient, createdAt time.Time, cap int) (bool, error)
}

// NewMCPService validates the configuration. When MCP is enabled the resource
// is required: it binds every grant, and its host is the public origin host
// that an https redirect may not name (ADR 0023 self-redirect rule).
func NewMCPService(repository MCPRepository, options MCPOptions, clock func() time.Time) (*MCPService, error) {
	if repository == nil {
		return nil, errors.New("mcp repository is required")
	}
	if options.Enabled && options.Resource == "" {
		return nil, errors.New("mcp resource is required when mcp is enabled")
	}
	publicHost := ""
	if options.Resource != "" {
		u, err := url.Parse(options.Resource)
		if err != nil || u.Hostname() == "" {
			return nil, errors.New("mcp resource must be an absolute URL")
		}
		publicHost = u.Hostname()
	}
	if clock == nil {
		clock = time.Now
	}
	store, _ := repository.(MCPClientStore)
	sweeper, _ := repository.(MCPSweepStore)
	grants, _ := repository.(MCPGrantStore)
	installation, _ := repository.(MCPInstallationStore)
	if options.InstallationSwitch != nil {
		installation = options.InstallationSwitch
	}
	return &MCPService{
		repository: repository, clock: clock, enabled: options.Enabled, resource: options.Resource,
		publicHost: publicHost, clientStore: store, sweeper: sweeper, grantStore: grants,
		installation: installation, dbTimeout: options.DBTimeout,
	}, nil
}

func (s *MCPService) now() time.Time { return s.clock().UTC().Truncate(time.Microsecond) }

// MCPAuthorization is an approved consent: the signed-in human, the browser
// session that approved it and the validated authorization request.
type MCPAuthorization struct {
	Principal                                                 Principal
	SessionToken                                              string
	ClientID, RedirectURI, CodeChallenge, CodeChallengeMethod string
	Resource                                                  string
	Scopes                                                    []string
}

// MCPIssuedCode is the plaintext authorization code; it is never stored.
type MCPIssuedCode struct {
	Code      string
	ExpiresAt time.Time
}

// IssueAuthorizationCode stores a single-use PKCE code for an approved
// consent. The approving session must still exist under the account and
// credential share locks, so a password replacement or deletion that already
// signed the browser out cannot be followed by a fresh code. No HTTP route
// calls it: production codes come only from the consent decision. It is kept
// as the direct issuance seam the tests use, and applies the same policy,
// answering ErrMCPAccessDenied where the decision redirects with access_denied.
func (s *MCPService) IssueAuthorizationCode(ctx context.Context, in MCPAuthorization) (MCPIssuedCode, error) {
	if !s.enabled {
		return MCPIssuedCode{}, ErrMCPDisabled
	}
	scopes, err := s.validAuthorization(in)
	if err != nil {
		return MCPIssuedCode{}, err
	}
	now := s.now()
	code, record, err := newAuthorizationCode(in.Principal.UserID, in.ClientID, in.RedirectURI,
		in.CodeChallenge, s.resource, scopes, now)
	if err != nil {
		return MCPIssuedCode{}, err
	}
	err = s.repository.WithMCPTransaction(ctx, func(tx MCPTransaction) error {
		if e := tx.LockLiveAccount(ctx, in.Principal.UserID); errors.Is(e, ErrNotFound) {
			return ErrUnauthenticated
		} else if e != nil {
			return e
		}
		active, e := tx.ActiveSessionExists(ctx, in.Principal.UserID, tokenDigest(in.SessionToken), now)
		if e != nil {
			return e
		}
		if !active {
			return ErrUnauthenticated
		}
		client, e := tx.FindClient(ctx, in.ClientID)
		if errors.Is(e, ErrNotFound) {
			return ErrMCPInvalidClient
		} else if e != nil {
			return e
		}
		if !MCPRedirectURIMatches(client.RedirectURIs, in.RedirectURI) {
			return ErrMCPInvalidRequest
		}
		allowed, e := allowedScopesLocked(ctx, tx, in.Principal.UserID)
		if e != nil {
			return e
		}
		if record.Scopes = intersectMCPScopes(record.Scopes, allowed); len(record.Scopes) == 0 {
			return ErrMCPAccessDenied
		}
		return tx.CreateAuthorizationCode(ctx, record)
	})
	if err != nil {
		return MCPIssuedCode{}, err
	}
	return MCPIssuedCode{Code: code, ExpiresAt: record.ExpiresAt}, nil
}

// newAuthorizationCode mints the plaintext code and the record that stores only
// its digest. IssueAuthorizationCode and DecideAuthorization share it, so an
// approved consent yields the same code record as a direct issuance.
func newAuthorizationCode(userID, clientID, redirectURI, codeChallenge, resource string, scopes []string, now time.Time) (string, MCPCodeRecord, error) {
	code, codeHash, err := newMCPSecret()
	if err != nil {
		return "", MCPCodeRecord{}, err
	}
	return code, MCPCodeRecord{
		CodeHash: codeHash, UserID: userID, ClientID: clientID, RedirectURI: redirectURI,
		CodeChallenge: codeChallenge, Resource: resource, Scopes: scopes,
		CreatedAt: now, ExpiresAt: now.Add(MCPCodeLifetime),
	}, nil
}

// validAuthorization checks everything that needs no database read and returns
// the normalized scopes.
func (s *MCPService) validAuthorization(in MCPAuthorization) ([]string, error) {
	if in.Principal.UserID == "" || in.SessionToken == "" {
		return nil, ErrUnauthenticated
	}
	if in.CodeChallengeMethod != "S256" || !mcpSecretPattern.MatchString(in.CodeChallenge) {
		return nil, ErrMCPInvalidRequest
	}
	if in.Resource != s.resource || in.ClientID == "" || in.RedirectURI == "" {
		return nil, ErrMCPInvalidRequest
	}
	return normalizeMCPScopes(in.Scopes)
}

// normalizeMCPScopes deduplicates and sorts the requested scopes and refuses an
// empty set or an unknown scope.
func normalizeMCPScopes(requested []string) ([]string, error) {
	scopes := make([]string, 0, 2)
	for _, scope := range requested {
		if scope != MCPScopeRead && scope != MCPScopeWrite {
			return nil, ErrMCPInvalidRequest
		}
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, ErrMCPInvalidRequest
	}
	slices.Sort(scopes)
	return scopes, nil
}

// MCPAccess is what a verified access token authorizes. It is resolved from
// the database on every request; Scopes is never empty.
type MCPAccess struct {
	GrantID, UserID, ClientID string
	Scopes                    []string
	// Allowed is the scopes the account's policy allows now (Scopes is the
	// subset the grant was consented for). It decides the step-up challenge at
	// the endpoint and never reaches a tool.
	Allowed []string
}

// VerifyAccessToken resolves a bearer token for the configured resource. The
// grant, its expiry, the account and the policy are re-read on every call, so
// a revocation, deletion or policy change takes effect on the next request.
// Scopes is the effective set: the grant's scopes the policy still allows.
func (s *MCPService) VerifyAccessToken(ctx context.Context, token string) (MCPAccess, error) {
	if !s.enabled {
		return MCPAccess{}, ErrMCPDisabled
	}
	if !mcpSecretPattern.MatchString(token) {
		return MCPAccess{}, ErrMCPInvalidToken
	}
	active, err := s.repository.FindActiveAccessToken(ctx, tokenDigest(token), s.now())
	if errors.Is(err, ErrNotFound) {
		return MCPAccess{}, ErrMCPInvalidToken
	}
	if err != nil {
		return MCPAccess{}, err
	}
	grant := active.Grant
	if grant.Resource != s.resource {
		return MCPAccess{}, ErrMCPInvalidToken
	}
	if !active.Policy.InstallationActive() {
		return MCPAccess{}, ErrMCPDisabled
	}
	// A switched-off account or user pauses the grant without revoking it.
	scopes := active.Policy.Effective(grant.Scopes)
	if len(scopes) == 0 {
		return MCPAccess{}, ErrMCPInvalidToken
	}
	return MCPAccess{GrantID: grant.ID, UserID: grant.UserID, ClientID: grant.ClientID, Scopes: scopes, Allowed: active.Policy.Allowed()}, nil
}

// allowedScopesLocked reads the account's policy after the account lock and
// returns the allowed scopes. An installation that is switched off is
// ErrMCPDisabled, exactly as the operator switch.
func allowedScopesLocked(ctx context.Context, tx MCPTransaction, userID string) ([]string, error) {
	policy, err := tx.ReadPolicy(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !policy.InstallationActive() {
		return nil, ErrMCPDisabled
	}
	return policy.Allowed(), nil
}

// newMCPSecret mints a 32-byte base64url secret and its SHA-256 digest. The
// plaintext never leaves the caller's response.
func newMCPSecret() (string, []byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, errors.New("mcp secret generation failed")
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	return secret, tokenDigest(secret), nil
}
