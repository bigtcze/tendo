package postgres

import (
	"context"
	"errors"

	"time"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// txBeginner is satisfied by the runtime connection pool. Transfer needs an
// explicit multi-statement transaction; the generated query wrapper alone
// cannot express the membership/subject locks.
type txBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

const subjectForUpdate = `
SELECT id::text, household_id::text, type, name, archived, created_at, updated_at, version, linked_user_id
FROM subjects
WHERE household_id = $1::uuid AND id = $2::uuid
FOR UPDATE`

func scanSubjectRow(source pgx.Row) (subject.Subject, error) {
	var r row
	if err := source.Scan(&r.ID, &r.HouseholdID, &r.Type, &r.Name, &r.Archived,
		&r.CreatedAt, &r.UpdatedAt, &r.Version, &r.LinkedUserID); err != nil {
		return subject.Subject{}, err
	}
	return toSubject(r), nil
}

// Transfer swaps a member's linked person with an existing unlinked person in
// one transaction. The membership row is locked first, so concurrent transfers
// for the same member serialize and the second observes that the current
// person is no longer linked. Both subjects are then locked in id order to
// avoid deadlocks with a concurrent rename. No item or history rows change:
// items keep their subject_id, and the previous person remains active.
func (r *Repository) Transfer(ctx context.Context, householdID, userID string, request subject.TransferRequest) (subject.TransferResult, error) {
	ids, err := parseTransferIDs(householdID, userID, request)
	if err != nil {
		return subject.TransferResult{}, err
	}
	tx, err := r.beginTransfer(ctx)
	if err != nil {
		return subject.TransferResult{}, err
	}
	defer rollbackTransfer(tx)
	if err := lockTransferMembership(ctx, tx, ids); err != nil {
		return subject.TransferResult{}, err
	}
	locked, err := lockTransferSubjects(ctx, tx, ids)
	if err != nil {
		return subject.TransferResult{}, err
	}
	current, target, err := lockedTransferSubjects(locked, ids)
	if err != nil {
		return subject.TransferResult{}, err
	}
	if err := validateTransfer(current, target, ids.user, request); err != nil {
		return subject.TransferResult{}, err
	}
	previous, err := unlinkTransferCurrent(ctx, tx, ids)
	if err != nil {
		return subject.TransferResult{}, err
	}
	person, err := linkTransferTarget(ctx, tx, ids)
	if err != nil {
		return subject.TransferResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return subject.TransferResult{}, errPersistence
	}
	return subject.TransferResult{Person: person, Previous: previous}, nil
}

type transferIDs struct {
	household, user, target, current pgtype.UUID
}

func parseTransferIDs(householdID, userID string, request subject.TransferRequest) (transferIDs, error) {
	hid, ok1 := parseUUID(householdID)
	user, ok2 := parseUUID(userID)
	target, ok3 := parseUUID(request.SubjectID)
	current, ok4 := parseUUID(request.CurrentSubjectID)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return transferIDs{}, subject.ErrNotFound
	}
	return transferIDs{household: hid, user: user, target: target, current: current}, nil
}

func (r *Repository) beginTransfer(ctx context.Context) (pgx.Tx, error) {
	beginner, ok := r.db.(txBeginner)
	if !ok {
		return nil, errPersistence
	}
	tx, err := beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, errPersistence
	}
	return tx, nil
}

func rollbackTransfer(tx pgx.Tx) {
	rollback, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(rollback)
}

func lockTransferMembership(ctx context.Context, tx pgx.Tx, ids transferIDs) error {
	var role string
	err := tx.QueryRow(ctx,
		`SELECT role FROM household_memberships WHERE user_id = $1::uuid AND household_id = $2::uuid FOR NO KEY UPDATE`,
		ids.user, ids.household).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.ErrNotFound
	}
	if err != nil {
		return errPersistence
	}
	return nil
}

func lockTransferSubjects(ctx context.Context, tx pgx.Tx, ids transferIDs) (map[string]subject.Subject, error) {
	first, second := ids.target, ids.current
	if uuidString(first) > uuidString(second) {
		first, second = second, first
	}
	locked := make(map[string]subject.Subject, 2)
	for _, id := range []pgtype.UUID{first, second} {
		found, err := scanSubjectRow(tx.QueryRow(ctx, subjectForUpdate, ids.household, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, subject.ErrNotFound
		}
		if err != nil {
			return nil, errPersistence
		}
		locked[found.ID] = found
	}
	return locked, nil
}

func lockedTransferSubjects(locked map[string]subject.Subject, ids transferIDs) (subject.Subject, subject.Subject, error) {
	current, ok := locked[uuidString(ids.current)]
	if !ok {
		return subject.Subject{}, subject.Subject{}, subject.ErrNotFound
	}
	target, ok := locked[uuidString(ids.target)]
	if !ok {
		return subject.Subject{}, subject.Subject{}, subject.ErrNotFound
	}
	return current, target, nil
}

func validateTransfer(current, target subject.Subject, user pgtype.UUID, request subject.TransferRequest) error {
	if current.LinkedUserID == nil || *current.LinkedUserID != uuidString(user) {
		return subject.ErrMemberPersonChanged
	}
	if target.Version != request.SubjectVersion || current.Version != request.CurrentSubjectVersion {
		return subject.ErrVersionMismatch
	}
	if target.ID == current.ID || target.Type != subject.TypePerson || target.Archived || target.LinkedUserID != nil {
		return &subject.ValidationError{Field: "subjectId", Code: "not_eligible"}
	}
	return nil
}

func unlinkTransferCurrent(ctx context.Context, tx pgx.Tx, ids transferIDs) (subject.Subject, error) {
	found, err := scanSubjectRow(tx.QueryRow(ctx, `
UPDATE subjects
SET linked_user_id = NULL, version = version + 1, updated_at = now()
WHERE household_id = $1::uuid AND id = $2::uuid
RETURNING id::text, household_id::text, type, name, archived, created_at, updated_at, version, linked_user_id`,
		ids.household, ids.current))
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, subject.ErrVersionMismatch
	}
	if err != nil {
		return subject.Subject{}, errPersistence
	}
	return found, nil
}

func linkTransferTarget(ctx context.Context, tx pgx.Tx, ids transferIDs) (subject.Subject, error) {
	found, err := scanSubjectRow(tx.QueryRow(ctx, `
UPDATE subjects
SET linked_user_id = $3::uuid, version = version + 1, updated_at = now()
WHERE household_id = $1::uuid AND id = $2::uuid
RETURNING id::text, household_id::text, type, name, archived, created_at, updated_at, version, linked_user_id`,
		ids.household, ids.target, ids.user))
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.Subject{}, subject.ErrVersionMismatch
	}
	if err != nil {
		return subject.Subject{}, errPersistence
	}
	return found, nil
}

// rollbackTimeout bounds cleanup after the request context is gone.
const rollbackTimeout = time.Second
