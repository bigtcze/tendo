package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rollbackTimeout bounds the best-effort rollback of a failed command
// transaction.
const rollbackTimeout = time.Second

// InstanceAdminRepository persists the installation-wide administrator
// capability through the runtime role. The role may SELECT and INSERT
// instance_admins only, so the recovery command cannot rewrite or remove an
// existing grant.
type InstanceAdminRepository struct {
	pool *pgxpool.Pool
}

func NewInstanceAdminRepository(pool *pgxpool.Pool) *InstanceAdminRepository {
	return &InstanceAdminRepository{pool: pool}
}

// GrantInstanceAdmin serializes grants on the installation row, refuses before
// setup completed and inserts the grant once. On an already-granted account the
// insert is a no-op that still reports the account's user ID.
func (r *InstanceAdminRepository) GrantInstanceAdmin(ctx context.Context, login string) (household.InstanceAdminGrant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return household.InstanceAdminGrant{}, errors.New("instance administrator persistence failed")
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	// FOR UPDATE needs UPDATE privilege on at least one column; the runtime role
	// holds UPDATE(setup_required) on installation_state, which is exactly the
	// column this command must not change.
	var setupRequired bool
	if err = tx.QueryRow(ctx, `SELECT setup_required FROM installation_state WHERE singleton=true FOR UPDATE`).Scan(&setupRequired); err != nil {
		return household.InstanceAdminGrant{}, errors.New("instance administrator persistence failed")
	}
	if setupRequired {
		return household.InstanceAdminGrant{}, household.ErrSetupIncomplete
	}
	var userID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM user_accounts WHERE login=$1`, login).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.InstanceAdminGrant{}, household.ErrAccountNotFound
	}
	if err != nil {
		return household.InstanceAdminGrant{}, errors.New("instance administrator persistence failed")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO instance_admins(user_id, granted_at, grant_source) VALUES($1::uuid, now(), 'cli') ON CONFLICT (user_id) DO NOTHING`, userID)
	if err != nil {
		return household.InstanceAdminGrant{}, errors.New("instance administrator persistence failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return household.InstanceAdminGrant{}, errors.New("instance administrator persistence failed")
	}
	return household.InstanceAdminGrant{UserID: userID, Granted: tag.RowsAffected() == 1}, nil
}

// SetupCompleteWithoutInstanceAdmin reads both facts in one statement so the
// startup warning cannot observe a half-written first-run installation.
func (r *InstanceAdminRepository) SetupCompleteWithoutInstanceAdmin(ctx context.Context) (bool, error) {
	var missing bool
	err := r.pool.QueryRow(ctx, `SELECT NOT setup_required AND NOT EXISTS(SELECT 1 FROM instance_admins)
		FROM installation_state WHERE singleton=true`).Scan(&missing)
	return missing, err
}
