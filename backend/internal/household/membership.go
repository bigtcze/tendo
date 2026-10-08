package household

import (
	"context"
	"encoding/base64"
	"errors"
)

var ErrForbidden = errors.New("owner_required")

const RoleOwner = "owner"
const RoleMember = "member"

type Member struct {
	UserID string
	Login  string
	Role   string
}
type MembershipRepository interface {
	GetMembership(context.Context, string, string) (string, error)
	AddInvitedMember(context.Context, string, string) error
	ListMembers(context.Context, string, string, int) ([]Member, string, error)
}
type MembershipService struct{ repository MembershipRepository }

func NewMembershipService(repository MembershipRepository) *MembershipService {
	return &MembershipService{repository}
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
	b, e := base64.RawURLEncoding.DecodeString(cursor[2:])
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
	if _, e := DecodeMemberCursor(cursor); e != nil {
		return nil, "", e
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if _, e := s.GetMembership(ctx, actorID, householdID); e != nil {
		return nil, "", e
	}
	return s.repository.ListMembers(ctx, householdID, cursor, limit)
}
