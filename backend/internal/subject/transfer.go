package subject

import (
	"context"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// GetMemberPerson returns the canonical person subject of a household member.
// Any household member may read it; the caller must be a member of the same
// household. A malformed, foreign, non-member or unlinked user is not found.
func (s *Service) GetMemberPerson(ctx context.Context, actorUserID, householdID, memberUserID string) (Subject, error) {
	if !ids.IsCanonicalUUID(memberUserID) {
		return Subject{}, ErrNotFound
	}
	householdID, err := s.check(ctx, actorUserID, householdID)
	if err != nil {
		return Subject{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	found, err := s.repository.GetMemberPerson(ctx, householdID, strings.ToLower(memberUserID))
	if err != nil {
		return Subject{}, mapRepoErr(err)
	}
	return found, nil
}

// Transfer makes an existing unlinked person subject the person of a household
// member. Any household member may transfer, because subjects are shared
// household data; the link itself never authorizes anything. The repository
// performs the locks, revalidation and writes in one transaction.
func (s *Service) Transfer(ctx context.Context, actorUserID, householdID, memberUserID string, request TransferRequest) (TransferResult, error) {
	if !ids.IsCanonicalUUID(request.SubjectID) || !ids.IsCanonicalUUID(request.CurrentSubjectID) ||
		!ids.IsCanonicalUUID(memberUserID) {
		return TransferResult{}, ErrNotFound
	}
	householdID, err := s.check(ctx, actorUserID, householdID)
	if err != nil {
		return TransferResult{}, err
	}
	if request.SubjectVersion < 1 || request.CurrentSubjectVersion < 1 {
		return TransferResult{}, ErrVersionMismatch
	}
	request.SubjectID = strings.ToLower(request.SubjectID)
	request.CurrentSubjectID = strings.ToLower(request.CurrentSubjectID)
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	result, err := s.repository.Transfer(ctx, householdID, strings.ToLower(memberUserID), request)
	if err != nil {
		return TransferResult{}, mapRepoErr(err)
	}
	return result, nil
}
