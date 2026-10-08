package household

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
)

const membershipRepositoryBudget = 5 * time.Second

var ErrForbidden = errors.New("owner_required")

const RoleOwner = "owner"
const RoleMember = "member"

type Member struct {
	UserID string
	Login  string
	Role   string
}
type MembershipRecord struct{ UserID, Role string }
type MembershipRepository interface {
	GetMembership(context.Context, string, string) (string, error)
	HasMembershipElsewhere(context.Context, string, string) (bool, error)
	AddInvitedMember(context.Context, string, string) error
	ListMembers(context.Context, string, string, int) ([]MembershipRecord, string, error)
}
type LoginLookup interface {
	LoginsByUserIDs(context.Context, []string) (map[string]string, error)
}
type MembershipService struct {
	repository MembershipRepository
	logins     LoginLookup
}

func NewMembershipService(repository MembershipRepository, logins ...LoginLookup) *MembershipService {
	service := &MembershipService{repository: repository}
	if len(logins) > 0 {
		service.logins = logins[0]
	}
	return service
}
func EncodeMemberCursor(id string) string {
	return "m1" + base64.RawURLEncoding.EncodeToString([]byte(id))
}
func DecodeMemberCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) < 3 || cursor[:2] != "m1" {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(cursor[2:])
	if e != nil || len(b) != 36 || !validUUID(string(b)) {
		return "", &ValidationError{"cursor", "invalid_format"}
	}
	return string(b), nil
}
func (s *MembershipService) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	if !validUUID(userID) || !validUUID(householdID) {
		return "", ErrNotFound
	}
	role, e := s.repository.GetMembership(ctx, userID, householdID)
	if errors.Is(e, ErrNotFound) {
		return "", ErrNotFound
	}
	if e != nil {
		return "", e
	}
	if role != RoleOwner && role != RoleMember {
		return "", ErrNotFound
	}
	return role, nil
}
func (s *MembershipService) HasMembershipElsewhere(ctx context.Context, userID, householdID string) (bool, error) {
	if !validUUID(userID) || !validUUID(householdID) {
		return false, ErrNotFound
	}
	return s.repository.HasMembershipElsewhere(ctx, userID, householdID)
}
func (s *MembershipService) AddInvitedMember(ctx context.Context, userID, householdID string) error {
	if !validUUID(userID) || !validUUID(householdID) {
		return ErrNotFound
	}
	return s.repository.AddInvitedMember(ctx, userID, householdID)
}
func (s *MembershipService) RequireOwner(ctx context.Context, userID, householdID string) error {
	role, e := s.GetMembership(ctx, userID, householdID)
	if e != nil {
		return e
	}
	if role != RoleOwner {
		return ErrForbidden
	}
	return nil
}
func (s *MembershipService) ListMembers(ctx context.Context, actorID, householdID, cursor string, limit int) ([]Member, string, error) {
	bounded, cancel := context.WithTimeout(ctx, membershipRepositoryBudget)
	defer cancel()
	decodedCursor, e := DecodeMemberCursor(cursor)
	if e != nil {
		return nil, "", e
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if _, e := s.GetMembership(bounded, actorID, householdID); e != nil {
		return nil, "", e
	}
	records, next, e := s.repository.ListMembers(bounded, householdID, decodedCursor, limit)
	if e != nil {
		return nil, "", e
	}
	if s.logins == nil {
		return nil, "", errors.New("member login lookup unavailable")
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.UserID)
	}
	logins, e := s.logins.LoginsByUserIDs(bounded, ids)
	if e != nil {
		return nil, "", e
	}
	members := make([]Member, 0, len(records))
	for _, record := range records {
		login, ok := logins[record.UserID]
		if !ok {
			return nil, "", errors.New("member login missing")
		}
		members = append(members, Member{UserID: record.UserID, Login: login, Role: record.Role})
	}
	return members, next, nil
}
