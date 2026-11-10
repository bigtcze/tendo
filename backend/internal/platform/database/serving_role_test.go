package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

func TestServingRoleFactsDecision(t *testing.T) {
	restricted := database.ServingRoleFacts{
		CurrentUser:           "tendo_r_1",
		SessionUser:           "tendo_r_1",
		RoleClosureRestricted: true,
	}
	for _, tc := range []struct {
		name   string
		mutate func(*database.ServingRoleFacts)
		want   string
	}{
		{name: "restricted role", mutate: func(*database.ServingRoleFacts) {}},
		{
			name:   "session mismatch",
			mutate: func(f *database.ServingRoleFacts) { f.CurrentUser = "tendo_owner" },
			want:   "database serving role is not the authenticated session role",
		},
		{
			name:   "elevated role in closure",
			mutate: func(f *database.ServingRoleFacts) { f.RoleClosureRestricted = false },
			want:   "database serving role has elevated privileges or role memberships",
		},
		{
			name:   "database create privilege",
			mutate: func(f *database.ServingRoleFacts) { f.CanCreateInDatabase = true },
			want:   "database serving role can create objects in the database",
		},
		{
			name:   "schema create privilege",
			mutate: func(f *database.ServingRoleFacts) { f.CanCreateInPublicSchema = true },
			want:   "database serving role can create objects in the public schema",
		},
		{
			name:   "schema owner membership",
			mutate: func(f *database.ServingRoleFacts) { f.InheritsSchemaOwner = true },
			want:   "database serving role inherits ownership of a schema",
		},
		{
			name:   "relation owner membership",
			mutate: func(f *database.ServingRoleFacts) { f.InheritsRelationOwner = true },
			want:   "database serving role inherits ownership of a relation",
		},
		{
			// Session identity is reported first because it invalidates every
			// other fact: another role's privileges were measured.
			name: "session mismatch precedes elevation",
			mutate: func(f *database.ServingRoleFacts) {
				f.SessionUser = "postgres"
				f.RoleClosureRestricted = false
				f.CanCreateInDatabase = true
			},
			want: "database serving role is not the authenticated session role",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := restricted
			tc.mutate(&facts)
			if got := facts.RestrictionViolation(); got != tc.want {
				t.Fatalf("violation=%q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateServingRoleAcceptsRuntimeAndRejectsMigrator(t *testing.T) {
	ctx := testContext(t)
	runtime := openPool(t, "TEST_DATABASE_URL")
	if err := database.ValidateServingRole(ctx, runtime); err != nil {
		t.Fatalf("runtime role refused: %v", err)
	}
	migrator := openPool(t, "TEST_DATABASE_ADMIN_URL")
	err := database.ValidateServingRole(ctx, migrator)
	if err == nil || err.Error() != "database serving role has elevated privileges or role memberships" {
		t.Fatalf("migrator role err=%v, want elevated-role refusal", err)
	}
	for _, leaked := range []string{"postgres://", "postgresql://", "tendo_m_", "tendo_r_"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaked connection detail %q: %v", leaked, err)
		}
	}
}

// TestValidateServingRoleAcceptsUnrelatedSchemaOwner pins that the ownership
// check generalizes beyond the public schema: a schema owned by the
// administrator, which the runtime role does not belong to, must not make the
// runtime role fail its isolation check.
func TestValidateServingRoleAcceptsUnrelatedSchemaOwner(t *testing.T) {
	ctx := testContext(t)
	admin := openPool(t, "TEST_DATABASE_ADMIN_URL")
	runtime := openPool(t, "TEST_DATABASE_URL")
	schema := pgx.Identifier{fmt.Sprintf("tendo_serving_role_probe_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create probe schema: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.Exec(dropCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop probe schema: %v", err)
		}
	})
	if err := database.ValidateServingRole(ctx, runtime); err != nil {
		t.Fatalf("runtime role refused over an unrelated schema: %v", err)
	}
}
