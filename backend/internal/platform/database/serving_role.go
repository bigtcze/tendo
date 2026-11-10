package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ServingRoleFacts is the isolation evidence a serving connection has to
// satisfy. It is a plain value so the decision below is table-testable without
// PostgreSQL.
type ServingRoleFacts struct {
	// CurrentUser and SessionUser must match so the role cannot be reached
	// through SET ROLE or connection pooling configuration.
	CurrentUser string
	SessionUser string
	// RoleClosureRestricted reports that neither the serving role nor any role
	// it can reach transitively has an elevated attribute and that none of them
	// is a predefined role.
	RoleClosureRestricted bool
	// CanCreateInDatabase and CanCreateInPublicSchema report CREATE privileges,
	// which allow DDL even without ownership.
	CanCreateInDatabase     bool
	CanCreateInPublicSchema bool
	// InheritsPublicSchemaOwner and InheritsPublicRelationOwner report
	// membership of the public schema owner or of a relation owner, because
	// membership alone grants their privileges.
	InheritsPublicSchemaOwner   bool
	InheritsPublicRelationOwner bool
}

// RestrictionViolation returns the first isolation violation, or an empty
// string when the role may serve traffic. Every message is specific and
// non-secret so startup logs stay actionable.
func (f ServingRoleFacts) RestrictionViolation() string {
	switch {
	case f.CurrentUser != f.SessionUser:
		return "database serving role is not the authenticated session role"
	case !f.RoleClosureRestricted:
		return "database serving role has elevated privileges or role memberships"
	case f.CanCreateInDatabase:
		return "database serving role can create objects in the database"
	case f.CanCreateInPublicSchema:
		return "database serving role can create objects in the public schema"
	case f.InheritsPublicSchemaOwner:
		return "database serving role inherits ownership of the public schema"
	case f.InheritsPublicRelationOwner:
		return "database serving role inherits ownership of a public schema relation"
	}
	return ""
}

// servingRoleFactsQuery collects every isolation fact in one statement so a
// role cannot change between checks. A role is considered elevated when it is
// predefined (oid < 16384, e.g. pg_write_all_data) or holds an attribute that
// grants DDL, more privileges or replication.
const servingRoleFactsQuery = `
WITH RECURSIVE role_closure(roleid) AS (
	SELECT oid FROM pg_roles WHERE rolname = current_user
	UNION
	SELECT m.roleid FROM pg_auth_members m JOIN role_closure c ON c.roleid = m.member
)
SELECT
	current_user::text,
	session_user::text,
	NOT EXISTS (
		SELECT 1 FROM role_closure c JOIN pg_roles r ON r.oid = c.roleid
		WHERE r.oid < 16384 OR r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls OR r.rolreplication
	),
	has_database_privilege(current_database(), 'CREATE'),
	has_schema_privilege('public', 'CREATE'),
	EXISTS (
		SELECT 1 FROM pg_namespace n
		WHERE n.nspname = 'public' AND pg_has_role(current_user, n.nspowner, 'MEMBER')
	),
	EXISTS (
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND pg_has_role(current_user, c.relowner, 'MEMBER')
	)`

// ValidateServingRole proves the pool is connected as a restricted runtime
// role before the application listens. It runs on every startup, with or
// without administrator credentials, so a native run or test cannot serve with
// an elevated or schema-owning role either.
func ValidateServingRole(ctx context.Context, pool *pgxpool.Pool) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var facts ServingRoleFacts
	if err := pool.QueryRow(bounded, servingRoleFactsQuery).Scan(
		&facts.CurrentUser,
		&facts.SessionUser,
		&facts.RoleClosureRestricted,
		&facts.CanCreateInDatabase,
		&facts.CanCreateInPublicSchema,
		&facts.InheritsPublicSchemaOwner,
		&facts.InheritsPublicRelationOwner,
	); err != nil {
		return errors.New("database serving role check failed")
	}
	if violation := facts.RestrictionViolation(); violation != "" {
		return errors.New(violation)
	}
	return nil
}
