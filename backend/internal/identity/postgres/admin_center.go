package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/household"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AssignmentEffects implements the identity-owned side of an instance-admin
// household assignment (ADR 0020). It is constructed with the household
// instance-admin transaction, so the account lock and default change commit or
// roll back with the membership insert.
type AssignmentEffects struct{ q *identitydb.Queries }

func NewAssignmentEffects(db identitydb.DBTX) *AssignmentEffects {
	return &AssignmentEffects{q: identitydb.New(db)}
}

// LockAssignableAccount locks the account row exactly like invitation
// acceptance and returns its login. A missing account is household.ErrNotFound.
func (e *AssignmentEffects) LockAssignableAccount(ctx context.Context, userID string) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return "", household.ErrNotFound
	}
	login, err := e.q.LockAssignableAccount(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", household.ErrNotFound
	}
	return login, err
}

// SetDefaultHouseholdIfUnset points an account without a default at the
// household it was just assigned to.
func (e *AssignmentEffects) SetDefaultHouseholdIfUnset(ctx context.Context, userID, householdID string) error {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return household.ErrNotFound
	}
	return e.q.SetDefaultHouseholdIfUnset(ctx, identitydb.SetDefaultHouseholdIfUnsetParams{UserID: user, HouseholdID: house})
}

// AccountDirectory is the read-only, identity-owned installation account
// directory used by the instance-admin center. It returns IDs and logins only.
type AccountDirectory struct{ q *identitydb.Queries }

func NewAccountDirectory(db identitydb.DBTX) *AccountDirectory {
	return &AccountDirectory{q: identitydb.New(db)}
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (d *AccountDirectory) ListDirectoryAccounts(ctx context.Context, page household.DirectoryPage) ([]household.DirectoryAccount, error) {
	var after pgtype.UUID
	if page.AfterUserID != "" {
		if err := after.Scan(page.AfterUserID); err != nil {
			return nil, &household.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, err := d.q.ListDirectoryAccounts(ctx, identitydb.ListDirectoryAccountsParams{
		Pattern: likeEscaper.Replace(page.Search), AfterID: after, AfterLogin: page.AfterLogin, RowLimit: int32(page.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]household.DirectoryAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, household.DirectoryAccount{UserID: row.ID, Login: row.Login})
	}
	return out, nil
}
