package postgres

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/bigtcze/tendo/backend/internal/subject/postgres/dbgen"
	"github.com/jackc/pgx/v5"
)

// MemberPersonProvisioner creates the canonical person subject for a
// membership using whatever transaction (or pool) it was constructed with.
// Identity injects the tx-bound instance so the person commits atomically with
// the membership. The partial unique index makes this idempotent under
// concurrent provisioning.
type MemberPersonProvisioner struct {
	db dbgen.DBTX
}

func NewMemberPersonProvisioner(db dbgen.DBTX) *MemberPersonProvisioner {
	return &MemberPersonProvisioner{db: db}
}

// GetMemberPerson returns the linked person subject for a member of the given
// household. Both the household and the user are part of the query, so foreign
// households and non-members are indistinguishable from a missing link.
func (r *Repository) GetMemberPerson(ctx context.Context, householdID, userID string) (subject.Subject, error) {
	hid, ok1 := parseUUID(householdID)
	user, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return subject.Subject{}, subject.ErrNotFound
	}
	found, err := scanSubjectRow(r.db.QueryRow(ctx, `
SELECT id::text, household_id::text, type, name, archived, created_at, updated_at, version, linked_user_id
FROM subjects
WHERE household_id = $1::uuid AND linked_user_id = $2::uuid AND type = 'person'`,
		hid, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, subject.ErrNotFound
	}
	if err != nil {
		return subject.Subject{}, errPersistence
	}
	return found, nil
}

// EnsureMemberPerson inserts one linked person when absent and verifies that a
// linked person now exists. It never matches or claims an unlinked person by
// name: the link is the membership identity.
func (p *MemberPersonProvisioner) EnsureMemberPerson(ctx context.Context, householdID, userID, name string) error {
	if _, err := p.db.Exec(ctx, `
INSERT INTO subjects (household_id, type, name, linked_user_id)
VALUES ($1::uuid, 'person', $3, $2::uuid)
ON CONFLICT (linked_user_id, household_id) WHERE linked_user_id IS NOT NULL DO NOTHING`,
		householdID, userID, name); err != nil {
		return errPersistence
	}
	var exists bool
	if err := p.db.QueryRow(ctx, `
SELECT EXISTS (
 SELECT 1 FROM subjects
 WHERE household_id = $1::uuid AND linked_user_id = $2::uuid AND type = 'person'
)`, householdID, userID).Scan(&exists); err != nil || !exists {
		return errPersistence
	}
	return nil
}
