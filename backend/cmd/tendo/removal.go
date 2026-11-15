package main

import (
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	itempostgres "github.com/bigtcze/tendo/backend/internal/item/postgres"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newMemberRemovalService wires each module's transactional removal effect
// (ADR 0019) into one household-owned removal transaction.
func newMemberRemovalService(pool *pgxpool.Pool) *household.MemberRemovalService {
	return household.NewMemberRemovalService(householdpostgres.NewMemberRemovalRepository(pool, householdpostgres.MemberRemovalEffectFactories{
		Invitations: func(tx householddb.DBTX) household.InvitationRevoker { return identitypostgres.NewRemovalEffects(tx) },
		Items: func(tx householddb.DBTX) household.ResponsibilityReleaser {
			return itempostgres.NewResponsibilityReleaser(tx)
		},
		Subjects: func(tx householddb.DBTX) household.MemberPersonUnlinker {
			return subjectpostgres.NewMemberPersonUnlinker(tx)
		},
		Resets: func(tx householddb.DBTX) household.PasswordResetRevoker {
			return identitypostgres.NewRemovalEffects(tx)
		},
		Accounts: func(tx householddb.DBTX) household.DefaultHouseholdReassigner {
			return identitypostgres.NewRemovalEffects(tx)
		},
	}), time.Now)
}
