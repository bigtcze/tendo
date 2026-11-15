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

// newInstanceAdminCenter wires the instance-admin household center (ADR 0020):
// the removal effects of ADR 0019 plus the assignment effects of the identity
// and subject modules, all on the instance-admin transaction.
func newInstanceAdminCenter(pool *pgxpool.Pool, logins household.LoginLookup, signIn household.SignInMethodsLookup) *household.InstanceAdminCenter {
	repository := householdpostgres.NewInstanceAdminCenterRepository(pool, memberRemovalEffectFactories(), householdpostgres.AssignmentEffectFactories{
		Accounts: func(tx householddb.DBTX) household.AssignableAccountLocker {
			return identitypostgres.NewAssignmentEffects(tx)
		},
		Defaults: func(tx householddb.DBTX) household.AssignmentDefaultSetter {
			return identitypostgres.NewAssignmentEffects(tx)
		},
		Persons: func(tx householddb.DBTX) household.AssignmentPersonProvisioner {
			return subjectpostgres.NewMemberPersonProvisioner(tx)
		},
	}, func(tx householddb.DBTX) household.AccountDirectory {
		return identitypostgres.NewAccountDirectory(tx)
	})
	return household.NewInstanceAdminCenter(repository, logins, signIn, time.Now)
}

// newMemberRemovalService wires each module's transactional removal effect
// (ADR 0019) into one household-owned removal transaction.
func newMemberRemovalService(pool *pgxpool.Pool) *household.MemberRemovalService {
	return household.NewMemberRemovalService(householdpostgres.NewMemberRemovalRepository(pool, memberRemovalEffectFactories()), time.Now)
}

func memberRemovalEffectFactories() householdpostgres.MemberRemovalEffectFactories {
	return householdpostgres.MemberRemovalEffectFactories{
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
	}
}
