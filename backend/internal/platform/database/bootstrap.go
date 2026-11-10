package database

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeRoleName is the role every migration grants privileges to. The
// embedded migrations name it literally, so the bootstrap cannot parameterize
// it without breaking the privilege contract.
const runtimeRoleName = "tendo"

// roleBootstrapAdvisoryLock serializes concurrent role bootstrap runs across
// application instances. It is deliberately distinct from the migration
// session lock so the two steps can never block each other.
const roleBootstrapAdvisoryLock int64 = 784193215

// BootstrapRole creates the restricted runtime role when it is absent and
// applies the idempotent database and schema grants an administrator has to
// perform. An existing role is never altered: its password and attributes
// belong to the operator, and an existing role that is elevated or a member of
// any other role is refused instead of silently repaired.
//
// databaseURL is the restricted connection string the application serves with;
// only its role name and password are used. The caller supplies an
// administrator pool, which is never used for request traffic.
func BootstrapRole(ctx context.Context, admin *pgxpool.Pool, databaseURL string) error {
	password, err := runtimeRolePassword(databaseURL)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := admin.Acquire(bounded)
	if err != nil {
		return errors.New("database administrator connection failed")
	}
	defer conn.Release()
	tx, err := conn.Begin(bounded)
	if err != nil {
		return errors.New("database administrator transaction failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(bounded, `SELECT pg_advisory_xact_lock($1)`, roleBootstrapAdvisoryLock); err != nil {
		return errors.New("database role bootstrap lock failed")
	}
	exists, err := runtimeRoleExists(bounded, tx)
	if err != nil {
		return err
	}
	if !exists {
		if err := createRuntimeRole(bounded, tx, password); err != nil {
			return err
		}
	}
	if err := applyRuntimeGrants(bounded, tx); err != nil {
		return err
	}
	if err := tx.Commit(bounded); err != nil {
		return errors.New("database role bootstrap commit failed")
	}
	return nil
}

// runtimeRolePassword extracts the serving password from the restricted URL.
// The role name is fixed, so a URL naming another role is a configuration
// error rather than something to reconcile.
func runtimeRolePassword(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil || u.User == nil || u.User.Username() != runtimeRoleName {
		return "", errors.New("DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set")
	}
	password, _ := u.User.Password()
	if password == "" {
		return "", errors.New("DATABASE_URL must include the role tendo password when TENDO_DATABASE_ADMIN_URL is set")
	}
	return password, nil
}

// runtimeRoleExists reports whether the runtime role is usable, refusing an
// existing role that holds elevated attributes or belongs to any other role.
// NOINHERIT does not prevent SET ROLE, so membership is a privilege escalation
// path that must be treated like an elevated attribute.
func runtimeRoleExists(ctx context.Context, tx pgx.Tx) (bool, error) {
	var elevated bool
	err := tx.QueryRow(ctx, `
		SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolbypassrls OR rolreplication
		       OR EXISTS (SELECT 1 FROM pg_auth_members m WHERE m.member = r.oid)
		FROM pg_roles r WHERE rolname = $1`, runtimeRoleName).Scan(&elevated)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, errors.New("database runtime role lookup failed")
	case elevated:
		return false, errors.New("database runtime role tendo has elevated privileges or role memberships")
	default:
		return true, nil
	}
}

// createRuntimeRole builds the CREATE ROLE statement server-side so the
// password is quoted by PostgreSQL and never appears in a Go string, log line
// or wrapped error. The generated statement is not included in errors.
func createRuntimeRole(ctx context.Context, tx pgx.Tx, password string) error {
	var statement string
	if err := tx.QueryRow(ctx, `
		SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD %L', $1, $2)`,
		runtimeRoleName, password).Scan(&statement); err != nil {
		return errors.New("database runtime role creation failed")
	}
	if _, err := tx.Exec(ctx, statement); err != nil {
		return errors.New("database runtime role creation failed")
	}
	return nil
}

// applyRuntimeGrants is idempotent and safe to repeat on every startup and on
// restore into an empty database: it only grants what the runtime role needs
// to connect, use the schema and be granted table privileges by migrations.
func applyRuntimeGrants(ctx context.Context, tx pgx.Tx) error {
	var databaseName string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		return errors.New("database name lookup failed")
	}
	role := pgx.Identifier{runtimeRoleName}.Sanitize()
	database := pgx.Identifier{databaseName}.Sanitize()
	if _, err := tx.Exec(ctx, `GRANT CONNECT ON DATABASE `+database+` TO `+role); err != nil {
		return errors.New("database runtime role connect grant failed")
	}
	if _, err := tx.Exec(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`); err != nil {
		return errors.New("database public schema restriction failed")
	}
	if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA public TO `+role); err != nil {
		return errors.New("database runtime role schema grant failed")
	}
	return nil
}
