package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdhttp "github.com/bigtcze/tendo/backend/internal/household/httpapi"
	householdmcp "github.com/bigtcze/tendo/backend/internal/household/mcptool"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/identity/oidcprovider"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	itemhttp "github.com/bigtcze/tendo/backend/internal/item/httpapi"
	itemmcp "github.com/bigtcze/tendo/backend/internal/item/mcptool"
	itempostgres "github.com/bigtcze/tendo/backend/internal/item/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/httporigin"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
	subjecthttp "github.com/bigtcze/tendo/backend/internal/subject/httpapi"
	subjectmcp "github.com/bigtcze/tendo/backend/internal/subject/mcptool"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimePool interface {
	httpx.Pinger
	Close()
}

type runtimeResources struct {
	server *http.Server
	pool   runtimePool
}

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "instance-admin" {
		os.Exit(runInstanceAdmin(os.Args[2:], os.Getenv))
	}
	if len(os.Args) >= 2 && os.Args[1] == "users" {
		os.Exit(runUsers(os.Args[2:], os.Getenv, terminalPasswordPrompt, os.Stdout, os.Stderr))
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "migrate":
			// Used by the native end-to-end runner, where DATABASE_URL is the
			// administrator connection.
			exitOnError("migration failed", migrate)
			return
		case "ensure-role":
			exitOnError("role bootstrap failed", ensureRole)
			return
		case "healthcheck":
			exitOnError("healthcheck failed", healthcheck)
			return
		}
	}
	exitOnError("runtime failed", run)
}

// exitOnError reports the operator-facing failure and exits non-zero. The
// command functions return value-free messages, so credentials never reach the
// log.
func exitOnError(message string, command func() error) {
	if err := command(); err != nil {
		slog.Error(message, "error", err)
		os.Exit(1)
	}
}

func migrate() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return errors.New("migration database initialization failed")
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("migration database unavailable")
	}
	return database.Migrate(ctx, pool)
}

// registerAPI composes every /api/v1 route with its authentication middleware.
// It performs no database I/O, so tests can verify the composed security
// boundary against the OpenAPI contract without PostgreSQL.
func registerAPI(routes chi.Router, cfg config.Config, pool *pgxpool.Pool, overrides compositionOverrides) error {
	memberPersonFactory := func(tx householddb.DBTX) identityapp.MemberPersonProvisioner {
		return subjectpostgres.NewMemberPersonProvisioner(tx)
	}
	var identityRepository *identitypostgres.Repository
	identityRepository = identitypostgres.NewWithInvitations(pool, func(queries *householddb.Queries) identityapp.OwnerHouseholdService {
		return household.NewBootstrapService(householdpostgres.NewBootstrapRepository(queries))
	}, memberPersonFactory, func(tx householddb.DBTX) identitypostgres.InvitationHouseholdService {
		return householdpostgres.MembershipServiceFor(tx, identityRepository)
	})
	passwordGate := security.NewPasswordGate(2)
	setupService := identityapp.NewSetupService(identityRepository)
	setupService.SetPasswordHasher(passwordGate.HashPassword)
	identityhttp.New(setupService, cfg.SetupToken).Register(routes)
	sessions, err := identityapp.NewSessionService(identityRepository, time.Now)
	if err != nil {
		return err
	}
	sessions.SetPasswordVerifier(passwordGate.VerifyPassword)
	sessionHandler := identityhttp.NewSession(sessions, cfg.PublicURL)
	sessionHandler.Register(routes)
	oidcService, err := newOIDCService(cfg, identityRepository, sessions, time.Now)
	if err != nil {
		return err
	}
	newOIDCHandler(oidcService, sessionHandler, cfg.PublicURL).Register(routes)
	principalID := func(ctx context.Context) (string, bool) {
		principal, ok := identityhttp.PrincipalFromContext(ctx)
		return principal.UserID, ok
	}
	householdService := household.NewService(householdpostgres.NewRepository(pool))
	householdhttp.New(householdService, sessionHandler.RequireSession, principalID).Register(routes)
	membershipService := householdpostgres.MembershipServiceFor(pool, identityRepository)
	householdhttp.NewMembers(membershipService, sessionHandler.RequireSession, principalID).Register(routes)
	passwordResetRepository := identitypostgres.NewPasswordResetRepository(pool, func(tx householddb.DBTX) identitypostgres.PasswordResetHouseholdService {
		return householdpostgres.MembershipServiceFor(tx)
	})
	accountService := householdpostgres.AccountServiceFor(pool, identityRepository, identityRepository, passwordResetRepository, time.Now)
	memberRemovalService := newMemberRemovalService(pool)
	householdhttp.NewAccounts(household.NewAccountCenter(accountService, memberRemovalService), sessionHandler.RequireSession, principalID).Register(routes)
	householdhttp.NewAdmin(newInstanceAdminCenter(pool, identityRepository, identityRepository), sessionHandler.RequireSession, principalID).Register(routes)
	invitationRepo := identitypostgres.NewInvitationRepository(pool, func(tx householddb.DBTX) identitypostgres.InvitationHouseholdService {
		return householdpostgres.MembershipServiceFor(tx, identityRepository)
	}, memberPersonFactory)
	invitationService, err := identityapp.NewInvitationService(invitationRepo, time.Now)
	if err != nil {
		return err
	}
	invitationService.SetPasswordHasher(passwordGate.HashPassword)
	identityhttp.NewInvitations(invitationService, sessionHandler.RequireSession, principalID).Register(routes)
	passwordResetService, err := identityapp.NewPasswordResetService(passwordResetRepository, time.Now)
	if err != nil {
		return err
	}
	passwordResetService.SetPasswordHasher(passwordGate.HashPassword)
	identityhttp.NewPasswordReset(passwordResetService, sessionHandler.RequireSession, principalID).Register(routes)
	passwordChangeService, err := identityapp.NewPasswordChangeService(identitypostgres.NewPasswordChangeRepository(pool), time.Now)
	if err != nil {
		return err
	}
	passwordChangeService.SetPasswordHasher(passwordGate.HashPassword)
	passwordChangeService.SetPasswordVerifier(passwordGate.VerifyPassword)
	identityhttp.NewPasswordChange(passwordChangeService, sessionHandler).Register(routes)
	// Subjects authorize through household membership, never through cross-module SQL.
	subjectService := subjectapp.NewService(subjectpostgres.NewRepository(pool), subjectMembership(householdService.Get), subjectMembers(membershipService.GetMembership))
	subjecthttp.New(subjectService, sessionHandler.RequireSession, principalID).Register(routes)
	// Items reach households and subjects only through injected application-service adapters.
	itemNow := time.Now
	if cfg.TestClockNow != nil {
		fixed := *cfg.TestClockNow
		itemNow = func() time.Time { return fixed }
		slog.Warn("test business-date clock enabled", "fixed_instant", fixed.Format(time.RFC3339), "scope", "item_business_dates_only")
	}
	itemService := itemapp.NewService(itempostgres.NewRepository(pool), itemHouseholds(householdService.Get), itemSubjects(subjectService.Get), itemMembers(membershipService.GetMembership), itemNow)
	itemhttp.New(itemService, sessionHandler.RequireSession, principalID).Register(routes)
	return registerMCP(routes, cfg, pool, sessionHandler, mcpTools(householdService, itemService, subjectService), overrides)
}

// compositionOverrides carries test-only seams into the production composition.
// Production passes the zero value, so every field stays nil and the real
// database-backed implementation is used. It is passed explicitly, never held
// in a package-level variable.
type compositionOverrides struct {
	// installationSwitch, when set, answers the MCP installation switch in place
	// of the database read.
	installationSwitch identityapp.MCPInstallationStore
}

// registerMCP mounts the MCP OAuth endpoints and the Streamable HTTP endpoint.
// While TENDO_MCP_ENABLED is false, or the installation switch is off, every
// gated MCP route answers the unknown-path 404 (ADR 0023, slice B1b2).
func registerMCP(routes chi.Router, cfg config.Config, pool *pgxpool.Pool, sessions *identityhttp.SessionHandler, tools []identityhttp.MCPTool, overrides compositionOverrides) error {
	mcpService, err := newMCPService(cfg, pool, overrides)
	if err != nil {
		return err
	}
	identityhttp.NewMCP(mcpService, sessions, cfg.PublicURL).Register(routes)
	// Connected apps work whether or not TENDO_MCP_ENABLED is set: a paused
	// grant can still be listed and disconnected.
	identityhttp.NewMCPGrants(mcpService, sessions).Register(routes)
	// The account policy API (ADR 0023, slice B1b1) works whether or not
	// TENDO_MCP_ENABLED is set; the flag only empties the effective scopes.
	policyService, err := identityapp.NewMCPPolicyService(identitypostgres.NewMCPRepository(pool), cfg.MCPEnabled, time.Now)
	if err != nil {
		return err
	}
	identityhttp.NewMCPPolicy(policyService, sessions).Register(routes)
	endpoint, err := identityhttp.NewMCPEndpoint(mcpService, cfg.PublicURL, cfg.DBTimeout, tools)
	if err != nil {
		return err
	}
	endpoint.Register(routes)
	return nil
}

// mcpTools is the complete MCP tool set. Every tool is an adapter over the
// same application service the HTTP API uses. No instance-admin, member,
// invitation, role, account or policy tool exists (#457); the composition test
// pins this list and each tool's scope.
func mcpTools(households householdmcp.Lister, items mcpItems, subjects mcpSubjects) []identityhttp.MCPTool {
	tools := []identityhttp.MCPTool{householdmcp.ListHouseholds(households)}
	tools = append(tools, itemmcp.ReadTools(items)...)
	tools = append(tools, subjectmcp.ReadTools(subjects)...)
	tools = append(tools, itemmcp.WriteTools(items)...)
	return append(tools, subjectmcp.WriteTools(subjects)...)
}

// mcpItems is the item service as the MCP tools see it: the pinned read and
// write ports and nothing else.
type mcpItems interface {
	itemmcp.Reader
	itemmcp.Writer
}

// mcpSubjects is the subject service as the MCP tools see it.
type mcpSubjects interface {
	subjectmcp.Reader
	subjectmcp.Writer
}

// newMCPService builds the installation's MCP service. The TENDO_MCP_ENABLED
// kill switch decides whether issuance and verification are on.
func newMCPService(cfg config.Config, pool *pgxpool.Pool, overrides compositionOverrides) (*identityapp.MCPService, error) {
	return identityapp.NewMCPService(identitypostgres.NewMCPRepository(pool), identityapp.MCPOptions{
		Enabled:            cfg.MCPEnabled,
		Resource:           cfg.PublicURL + "/mcp",
		DBTimeout:          cfg.DBTimeout,
		InstallationSwitch: overrides.installationSwitch,
	}, time.Now)
}

// newOIDCHandler keeps disabled OIDC as an untyped nil service; passing a nil
// *OIDCService directly would create a non-nil interface and enable the routes.
func newOIDCHandler(service *identityapp.OIDCService, sessions *identityhttp.SessionHandler, publicURL string) *identityhttp.OIDCHandler {
	if service == nil {
		return identityhttp.NewOIDC(nil, sessions, publicURL)
	}
	return identityhttp.NewOIDC(service, sessions, publicURL)
}

func newOIDCService(cfg config.Config, repo identityapp.OIDCRepository, sessions *identityapp.SessionService, clock func() time.Time) (*identityapp.OIDCService, error) {
	if cfg.OIDCIssuer == "" {
		return nil, nil
	}
	publicOrigin, _ := httporigin.Parse(cfg.PublicURL, true)
	provider := oidcprovider.New(cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, nil, clock, publicOrigin.Scheme == "http")
	return identityapp.NewOIDCService(repo, provider, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCDisplayName, cfg.PublicURL, clock, nil, sessions.VerifyCurrentPassword)
}

func newRuntimeServer(addr string, handler http.Handler, serviceTimeout time.Duration) *http.Server {
	writeTimeout := 15*time.Second + serviceTimeout + 5*time.Second
	if writeTimeout < 25*time.Second {
		writeTimeout = 25 * time.Second
	}
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}

func serve(ctx context.Context, listener net.Listener, resources runtimeResources, draining chan struct{}, shutdownTimeout time.Duration) error {
	return serveWithSweeper(ctx, listener, resources, draining, shutdownTimeout, nil)
}

// serveWithSweeper is serve with a background worker. stopSweeper runs after
// the HTTP server has drained and before the pool closes, so no retention step
// can touch a closed pool. A nil stopSweeper does nothing.
func serveWithSweeper(ctx context.Context, listener net.Listener, resources runtimeResources, draining chan struct{}, shutdownTimeout time.Duration, stopSweeper func(time.Duration) bool) error {
	stop := func(timeout time.Duration) {
		if stopSweeper != nil {
			// A sweep still running at the deadline is abandoned; the pool close
			// that follows is bounded by the same deadline, so the result is not
			// acted on here.
			_ = stopSweeper(timeout)
		}
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- resources.server.Serve(listener) }()
	select {
	case <-ctx.Done():
		close(draining)
		deadline := time.Now().Add(shutdownTimeout)
		shutdownCtx, cancel := context.WithDeadline(context.Background(), deadline)
		shutdownErr := resources.server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = resources.server.Close()
		}
		stop(time.Until(deadline))
		shutdownErr = boundedPoolClose(resources.pool, time.Until(deadline), shutdownErr)
		if shutdownErr != nil {
			return errors.New("runtime shutdown failed")
		}
		select {
		case err := <-serveErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return errors.New("HTTP server stopped unexpectedly")
			}
		default:
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			stop(shutdownTimeout)
			return boundedPoolClose(resources.pool, shutdownTimeout, nil)
		}
		_ = resources.server.Close()
		stop(shutdownTimeout)
		return boundedPoolClose(resources.pool, shutdownTimeout, errors.New("HTTP server stopped unexpectedly"))
	}
}

func boundedPoolClose(pool runtimePool, timeout time.Duration, primary error) error {
	closed := make(chan struct{})
	go func() { pool.Close(); close(closed) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return primary
	case <-timer.C:
		if primary != nil {
			return primary
		}
		return errors.New("runtime shutdown failed")
	}
}
