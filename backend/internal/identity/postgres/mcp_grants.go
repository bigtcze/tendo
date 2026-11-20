package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var errInvalidMCPRevocation = errors.New("invalid MCP revocation")

// revokeAccountMCPAccess revokes every live MCP grant of the account with an
// audited cause and deletes every authorization code and every access and
// refresh token of the account (ADR 0023). The caller already holds the
// account-side locks, so the statements run in the ADR 0023 order: codes, then
// grants, then token rows. A revoked grant keeps no secret.
func revokeAccountMCPAccess(ctx context.Context, q *identitydb.Queries, user pgtype.UUID, now time.Time, revocation identity.MCPRevocation) error {
	if !revocation.Reason.Valid() {
		return errInvalidMCPRevocation
	}
	var actor pgtype.UUID
	if revocation.ActorUserID != "" {
		if err := actor.Scan(revocation.ActorUserID); err != nil {
			return errInvalidMCPRevocation
		}
	}
	if err := q.DeleteAccountMCPCodes(ctx, user); err != nil {
		return err
	}
	if _, err := q.RevokeAccountMCPGrants(ctx, identitydb.RevokeAccountMCPGrantsParams{
		UserID:       user,
		RevokedAt:    ts(now.UTC()),
		RevokeReason: pgtype.Text{String: string(revocation.Reason), Valid: true},
		ActorUserID:  actor,
	}); err != nil {
		return err
	}
	if err := q.DeleteAccountMCPAccessTokens(ctx, user); err != nil {
		return err
	}
	return q.DeleteAccountMCPRefreshTokens(ctx, user)
}

// sweepLockTimeout bounds how long one retention step waits for a row lock.
// A step that waits longer yields and the next run retries it (ADR 0023).
const sweepLockTimeout = "100ms"

// CreateClientWithinCap inserts a registered client only while fewer than cap
// grantless clients exist. It is one statement outside any issuance transaction.
func (r *MCPRepository) CreateClientWithinCap(ctx context.Context, client identity.MCPClient, createdAt time.Time, cap int) (bool, error) {
	rows, err := identitydb.New(r.pool).CreateMCPClientWithinCap(ctx, identitydb.CreateMCPClientWithinCapParams{
		ClientID: client.ID, ClientName: client.Name, RedirectUris: client.RedirectURIs,
		CreatedAt: ts(createdAt.UTC()), ClientCap: int64(cap),
	})
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// SweepStep runs one retention step in its own transaction. Each batch is a
// bounded statement; the step loops while batches come back full, and it ends
// on the context. lock_timeout (55P03) yields the step without failing the run.
func (r *MCPRepository) SweepStep(ctx context.Context, step identity.MCPSweepStep, now time.Time) (int, error) {
	fn, ok := sweepStatements[step]
	if !ok {
		return 0, errors.New("unknown mcp sweep step")
	}
	total := 0
	for {
		deleted, full, err := r.sweepBatch(ctx, fn, now)
		total += deleted
		if err != nil {
			return total, err
		}
		if !full {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

// sweepBatchFunc runs one bounded batch. full reports that some statement hit
// the batch bound, so another batch may hold more rows.
type sweepBatchFunc func(q *identitydb.Queries, ctx context.Context, now time.Time) (deleted int64, full bool, err error)

// sweepBatch runs one batch in its own transaction with the lock timeout set.
func (r *MCPRepository) sweepBatch(ctx context.Context, fn sweepBatchFunc, now time.Time) (int, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, false, classifySweepError(ctx, err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '"+sweepLockTimeout+"'"); err != nil {
		return 0, false, classifySweepError(ctx, err)
	}
	n, full, err := fn(identitydb.New(tx), ctx, now)
	if err != nil {
		return 0, false, classifySweepError(ctx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, classifySweepError(ctx, err)
	}
	return int(n), full, nil
}

// classifySweepError maps driver errors to fixed identity-layer classes, so no
// pgx text reaches the sweep's callers. A finished context is reported as its
// own error, so a caller can tell a timeout or cancellation from a storage
// failure. The driver error is not wrapped, so the context error is the only
// text returned.
func classifySweepError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return identity.ErrMCPSweepLocked
	}
	return identity.ErrMCPSweepFailed
}

// sweepStatements maps each retention step to its bounded batch statement.
var sweepStatements = map[identity.MCPSweepStep]sweepBatchFunc{
	identity.MCPSweepAccessTokensExpired: singleSweep(func(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, error) {
		return q.SweepMCPAccessTokensExpired(ctx, ts(now))
	}),
	identity.MCPSweepRefreshTokensExpired: singleSweep(func(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, error) {
		return q.SweepMCPRefreshTokensExpired(ctx, ts(now))
	}),
	identity.MCPSweepEndedGrantSecrets: endedGrantSweep,
	identity.MCPSweepUnusedCodesExpired: singleSweep(func(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, error) {
		return q.SweepMCPUnusedCodesExpired(ctx, ts(now))
	}),
	identity.MCPSweepGrantlessClients: singleSweep(func(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, error) {
		return q.SweepMCPGrantlessClients(ctx, ts(now.Add(-identity.MCPGrantlessClientLifetime)))
	}),
}

// singleSweep adapts one bounded statement to a batch: full when it hit the bound.
func singleSweep(run func(*identitydb.Queries, context.Context, time.Time) (int64, error)) sweepBatchFunc {
	return func(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, bool, error) {
		n, err := run(q, ctx, now)
		return n, n == identity.MCPSweepBatchSize, err
	}
}

// endedGrantSweep deletes, in one transaction per batch, the codes, access
// tokens and refresh tokens of grants past expiry, in the ADR 0023 order: codes,
// then token rows. All three run every batch, so a short batch of one kind never
// hides a full batch of another.
func endedGrantSweep(q *identitydb.Queries, ctx context.Context, now time.Time) (int64, bool, error) {
	codes, err := q.SweepMCPEndedGrantCodes(ctx, ts(now))
	if err != nil {
		return 0, false, err
	}
	access, err := q.SweepMCPEndedGrantAccessTokens(ctx, ts(now))
	if err != nil {
		return 0, false, err
	}
	refresh, err := q.SweepMCPEndedGrantRefreshTokens(ctx, ts(now))
	if err != nil {
		return 0, false, err
	}
	bound := int64(identity.MCPSweepBatchSize)
	return codes + access + refresh, codes == bound || access == bound || refresh == bound, nil
}

// The service finds its registration and sweep stores by type assertion on the
// repository. These assertions fail the build if a method drifts, so the wiring
// cannot silently degrade to the fail-closed path.
var (
	_ identity.MCPClientStore = (*MCPRepository)(nil)
	_ identity.MCPSweepStore  = (*MCPRepository)(nil)
	_ identity.MCPRepository  = (*MCPRepository)(nil)
)

// MCPRepository is the PostgreSQL MCP grant store (ADR 0023).
type MCPRepository struct {
	pool interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
		identitydb.DBTX
	}
}

func NewMCPRepository(pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	identitydb.DBTX
}) *MCPRepository {
	return &MCPRepository{pool: pool}
}

// WithMCPTransaction runs one issuance at READ COMMITTED: every decision is
// read under the explicit row locks the transaction takes (ADR 0019).
func (r *MCPRepository) WithMCPTransaction(ctx context.Context, work func(identity.MCPTransaction) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err = work(&mcpTx{q: identitydb.New(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MCPRepository) FindActiveAccessToken(ctx context.Context, tokenHash []byte, now time.Time) (identity.MCPGrantRecord, error) {
	row, err := identitydb.New(r.pool).FindActiveMCPAccessToken(ctx, identitydb.FindActiveMCPAccessTokenParams{
		TokenHash: tokenHash, ExpiresAt: ts(now.UTC()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.MCPGrantRecord{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.MCPGrantRecord{}, err
	}
	return identity.MCPGrantRecord{
		ID: row.ID.String(), UserID: row.UserID.String(), ClientID: row.ClientID, Scopes: row.Scopes, Resource: row.Resource,
	}, nil
}

type mcpTx struct {
	q *identitydb.Queries
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}

func scanUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, identity.ErrNotFound
	}
	return id, nil
}

func (t *mcpTx) LockLiveAccount(ctx context.Context, userID string) error {
	user, err := scanUUID(userID)
	if err != nil {
		return err
	}
	if _, err = t.q.LockLiveMCPAccount(ctx, user); err != nil {
		return notFound(err)
	}
	_, err = t.q.ShareLocalCredential(ctx, user)
	return err
}

func (t *mcpTx) ActiveSessionExists(ctx context.Context, userID string, tokenHash []byte, now time.Time) (bool, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return false, nil
	}
	return t.q.UserSessionActive(ctx, identitydb.UserSessionActiveParams{UserID: user, TokenHash: tokenHash, ExpiresAt: ts(now.UTC())})
}

func (t *mcpTx) FindClient(ctx context.Context, clientID string) (identity.MCPClient, error) {
	row, err := t.q.FindMCPClient(ctx, clientID)
	if err != nil {
		return identity.MCPClient{}, notFound(err)
	}
	return identity.MCPClient{ID: row.ClientID, Name: row.ClientName, RedirectURIs: row.RedirectUris}, nil
}

func (t *mcpTx) CreateAuthorizationCode(ctx context.Context, code identity.MCPCodeRecord) error {
	user, err := scanUUID(code.UserID)
	if err != nil {
		return err
	}
	return t.q.CreateMCPAuthorizationCode(ctx, identitydb.CreateMCPAuthorizationCodeParams{
		CodeHash: code.CodeHash, UserID: user, ClientID: code.ClientID, RedirectUri: code.RedirectURI,
		CodeChallenge: code.CodeChallenge, Scopes: code.Scopes, Resource: code.Resource,
		CreatedAt: ts(code.CreatedAt), ExpiresAt: ts(code.ExpiresAt),
	})
}

func (t *mcpTx) FindCodeOwner(ctx context.Context, codeHash []byte) (string, error) {
	user, err := t.q.FindMCPAuthorizationCodeOwner(ctx, codeHash)
	if err != nil {
		return "", notFound(err)
	}
	return user.String(), nil
}

func (t *mcpTx) LockAuthorizationCode(ctx context.Context, codeHash []byte) (identity.MCPCodeRecord, error) {
	row, err := t.q.LockMCPAuthorizationCode(ctx, codeHash)
	if err != nil {
		return identity.MCPCodeRecord{}, notFound(err)
	}
	record := identity.MCPCodeRecord{
		CodeHash: row.CodeHash, UserID: row.UserID.String(), ClientID: row.ClientID, RedirectURI: row.RedirectUri,
		CodeChallenge: row.CodeChallenge, Scopes: row.Scopes, Resource: row.Resource,
		ExpiresAt: row.ExpiresAt.Time, Used: row.UsedAt.Valid,
	}
	if row.IssuedGrantID.Valid {
		record.IssuedGrantID = row.IssuedGrantID.String()
	}
	return record, nil
}

func (t *mcpTx) MarkAuthorizationCodeUsed(ctx context.Context, codeHash []byte, grantID string, now time.Time) error {
	grant, err := scanUUID(grantID)
	if err != nil {
		return err
	}
	rows, err := t.q.MarkMCPAuthorizationCodeUsed(ctx, identitydb.MarkMCPAuthorizationCodeUsedParams{
		CodeHash: codeHash, UsedAt: ts(now.UTC()), IssuedGrantID: grant,
	})
	if err == nil && rows != 1 {
		return errors.New("locked MCP authorization code was not marked used")
	}
	return err
}

func (t *mcpTx) CreateGrant(ctx context.Context, grant identity.MCPGrantRecord) (string, error) {
	user, err := scanUUID(grant.UserID)
	if err != nil {
		return "", err
	}
	id, err := t.q.CreateMCPGrant(ctx, identitydb.CreateMCPGrantParams{
		UserID: user, ClientID: grant.ClientID, Scopes: grant.Scopes, Resource: grant.Resource,
		CreatedAt: ts(grant.CreatedAt.UTC()), ExpiresAt: ts(grant.ExpiresAt.UTC()),
	})
	if err != nil {
		return "", err
	}
	if err = t.q.RecordMCPGrantCreated(ctx, id); err != nil {
		return "", err
	}
	return id.String(), nil
}

func (t *mcpTx) CreateTokens(ctx context.Context, issue identity.MCPTokenIssue) error {
	grant, err := scanUUID(issue.GrantID)
	if err != nil {
		return err
	}
	if err = t.q.CreateMCPAccessToken(ctx, identitydb.CreateMCPAccessTokenParams{
		TokenHash: issue.AccessHash, GrantID: grant, CreatedAt: ts(issue.CreatedAt.UTC()), ExpiresAt: ts(issue.AccessExpiresAt.UTC()),
	}); err != nil {
		return err
	}
	return t.q.CreateMCPRefreshToken(ctx, identitydb.CreateMCPRefreshTokenParams{
		TokenHash: issue.RefreshHash, GrantID: grant, CreatedAt: ts(issue.CreatedAt.UTC()), ExpiresAt: ts(issue.RefreshExpiresAt.UTC()),
	})
}

func (t *mcpTx) FindRefreshOwner(ctx context.Context, refreshHash []byte) (string, string, error) {
	row, err := t.q.FindMCPRefreshTokenOwner(ctx, refreshHash)
	if err != nil {
		return "", "", notFound(err)
	}
	return row.UserID.String(), row.GrantID.String(), nil
}

func (t *mcpTx) LockGrant(ctx context.Context, grantID string) (identity.MCPGrantRecord, error) {
	grant, err := scanUUID(grantID)
	if err != nil {
		return identity.MCPGrantRecord{}, err
	}
	if err = t.q.LockMCPGrantCodes(ctx, grant); err != nil {
		return identity.MCPGrantRecord{}, err
	}
	row, err := t.q.LockMCPGrant(ctx, grant)
	if err != nil {
		return identity.MCPGrantRecord{}, notFound(err)
	}
	return identity.MCPGrantRecord{
		ID: row.ID.String(), UserID: row.UserID.String(), ClientID: row.ClientID, Scopes: row.Scopes,
		Resource: row.Resource, ExpiresAt: row.ExpiresAt.Time, Revoked: row.RevokedAt.Valid,
	}, nil
}

func (t *mcpTx) LockRefreshToken(ctx context.Context, refreshHash []byte) (identity.MCPRefreshRecord, error) {
	row, err := t.q.LockMCPRefreshToken(ctx, refreshHash)
	if err != nil {
		return identity.MCPRefreshRecord{}, notFound(err)
	}
	return identity.MCPRefreshRecord{GrantID: row.GrantID.String(), ExpiresAt: row.ExpiresAt.Time, Consumed: row.ConsumedAt.Valid}, nil
}

func (t *mcpTx) ConsumeRefreshToken(ctx context.Context, refreshHash []byte, now time.Time) error {
	rows, err := t.q.ConsumeMCPRefreshToken(ctx, identitydb.ConsumeMCPRefreshTokenParams{TokenHash: refreshHash, ConsumedAt: ts(now.UTC())})
	if err == nil && rows != 1 {
		return errors.New("locked MCP refresh token was not consumed")
	}
	return err
}

// RevokeGrant revokes one grant in the ADR 0023 order. The caller already
// locked the grant's codes and the grant itself.
func (t *mcpTx) RevokeGrant(ctx context.Context, grantID string, now time.Time, revocation identity.MCPRevocation) error {
	if !revocation.Reason.Valid() {
		return errInvalidMCPRevocation
	}
	grant, err := scanUUID(grantID)
	if err != nil {
		return err
	}
	var actor pgtype.UUID
	if revocation.ActorUserID != "" {
		if actor, err = scanUUID(revocation.ActorUserID); err != nil {
			return errInvalidMCPRevocation
		}
	}
	if err = t.q.DeleteMCPGrantCodes(ctx, grant); err != nil {
		return err
	}
	if _, err = t.q.RevokeMCPGrant(ctx, identitydb.RevokeMCPGrantParams{
		GrantID: grant, RevokedAt: ts(now.UTC()),
		RevokeReason: pgtype.Text{String: string(revocation.Reason), Valid: true}, ActorUserID: actor,
	}); err != nil {
		return err
	}
	if err = t.q.DeleteMCPGrantAccessTokens(ctx, grant); err != nil {
		return err
	}
	return t.q.DeleteMCPGrantRefreshTokens(ctx, grant)
}
