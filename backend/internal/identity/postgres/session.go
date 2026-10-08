package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var _ identity.SessionRepository = (*Repository)(nil)
var _ household.LoginLookup = (*Repository)(nil)

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func (r *Repository) LoginsByUserIDs(ctx context.Context, ids []string) (map[string]string, error) {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if e := u.Scan(id); e != nil {
			return nil, e
		}
		uuids = append(uuids, u)
	}
	rows, e := identitydb.New(r.pool).MemberLogins(ctx, uuids)
	if e != nil {
		return nil, e
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.UserID] = row.Login
	}
	return out, nil
}

func (r *Repository) FindLogin(ctx context.Context, login string) (identity.LoginRecord, error) {
	row, err := identitydb.New(r.pool).FindLogin(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.LoginRecord{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.LoginRecord{}, err
	}
	return identity.LoginRecord{Principal: identity.Principal{UserID: row.UserID, Login: row.Login, DefaultHouseholdID: row.DefaultHouseholdID}, PasswordHash: row.PasswordHash}, nil
}

func (r *Repository) PruneExpired(ctx context.Context, userID string, now time.Time) error {
	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		return err
	}
	return identitydb.New(r.pool).DeleteExpiredSessions(ctx, identitydb.DeleteExpiredSessionsParams{UserID: id, ExpiresAt: timestamptz(now)})
}

func (r *Repository) CreateSession(ctx context.Context, s identity.NewSession) (string, error) {
	var id pgtype.UUID
	if err := id.Scan(s.UserID); err != nil {
		return "", err
	}
	return identitydb.New(r.pool).CreateSession(ctx, identitydb.CreateSessionParams{UserID: id, TokenHash: s.TokenHash, CreatedAt: timestamptz(s.CreatedAt), ExpiresAt: timestamptz(s.ExpiresAt)})
}

func (r *Repository) FindActive(ctx context.Context, tokenHash []byte, now time.Time) (identity.SessionInfo, error) {
	row, err := identitydb.New(r.pool).FindActiveSession(ctx, identitydb.FindActiveSessionParams{TokenHash: tokenHash, ExpiresAt: timestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.SessionInfo{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.SessionInfo{}, err
	}
	return identity.SessionInfo{ID: row.ID, ExpiresAt: row.ExpiresAt.Time.UTC(), Principal: identity.Principal{UserID: row.UserID, Login: row.Login, DefaultHouseholdID: row.DefaultHouseholdID}}, nil
}

func (r *Repository) DeleteByTokenHash(ctx context.Context, tokenHash []byte) error {
	return identitydb.New(r.pool).DeleteSession(ctx, tokenHash)
}
