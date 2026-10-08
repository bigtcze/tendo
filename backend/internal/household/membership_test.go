package household

import (
	"context"
	"errors"
	"testing"
)

const membershipActor = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
const membershipHouse = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"

type membershipFake struct {
	role      string
	err       error
	listCalls int
}

func (f *membershipFake) GetMembership(context.Context, string, string) (string, error) {
	return f.role, f.err
}
func (f *membershipFake) AddInvitedMember(context.Context, string, string) error { return nil }
func (f *membershipFake) HasMembershipElsewhere(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *membershipFake) ListMembers(context.Context, string, string, int) ([]MembershipRecord, string, error) {
	f.listCalls++
	return []MembershipRecord{{UserID: membershipActor, Role: RoleMember}}, "", nil
}

type memberLoginFake struct{}

func (memberLoginFake) LoginsByUserIDs(context.Context, []string) (map[string]string, error) {
	return map[string]string{membershipActor: "member"}, nil
}

type missingMemberLoginFake struct{}

func (missingMemberLoginFake) LoginsByUserIDs(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func TestMembershipAuthorizationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		err        error
		ownerErr   error
	}{{"owner", RoleOwner, nil, nil}, {"member", RoleMember, nil, ErrForbidden}, {"nonmember", "", ErrNotFound, ErrNotFound}, {"unknown", "mystery", nil, ErrNotFound}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &membershipFake{role: tc.role, err: tc.err}
			s := NewMembershipService(repo)
			role, e := s.GetMembership(context.Background(), membershipActor, membershipHouse)
			if tc.name == "owner" || tc.name == "member" {
				if e != nil || role != tc.role {
					t.Fatalf("role=%s err=%v", role, e)
				}
			} else if !errors.Is(e, ErrNotFound) {
				t.Fatalf("membership err=%v", e)
			}
			e = s.RequireOwner(context.Background(), membershipActor, membershipHouse)
			if !errors.Is(e, tc.ownerErr) {
				t.Fatalf("owner err=%v", e)
			}
		})
	}
}
func TestListMembersAllowsMemberAndValidatesOpaqueCursor(t *testing.T) {
	repo := &membershipFake{role: RoleMember}
	s := NewMembershipService(repo, memberLoginFake{})
	got, _, e := s.ListMembers(context.Background(), membershipActor, membershipHouse, "", 10)
	if e != nil || len(got) != 1 || repo.listCalls != 1 {
		t.Fatalf("members=%v calls=%d err=%v", got, repo.listCalls, e)
	}
	if _, _, e = s.ListMembers(context.Background(), membershipActor, membershipHouse, "bad", 10); e == nil {
		t.Fatal("malformed cursor accepted")
	}
	s = NewMembershipService(repo, missingMemberLoginFake{})
	if _, _, e = s.ListMembers(context.Background(), membershipActor, membershipHouse, "", 10); e == nil {
		t.Fatal("missing identity login was silently returned")
	}
}
