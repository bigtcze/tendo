package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bigtcze/tendo/backend/internal/household"
	householdhttp "github.com/bigtcze/tendo/backend/internal/household/httpapi"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/go-chi/chi/v5"
	"strings"
	"testing"
	"time"
)

type transportInvitationRepo struct {
	rows     map[string]identity.Invitation
	keys     map[string]string
	digests  map[string]string
	users    map[string]string
	defaults map[string]string
	members  map[string]map[string]string
	owner    string
	next     int
}
type transportInvitationTx struct{ r *transportInvitationRepo }

func newTransportInvitationRepo() *transportInvitationRepo {
	r := &transportInvitationRepo{rows: map[string]identity.Invitation{}, keys: map[string]string{}, digests: map[string]string{}, users: map[string]string{}, defaults: map[string]string{}, members: map[string]map[string]string{}, owner: "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"}
	r.users[r.owner] = "owner"
	return r
}
func (r *transportInvitationRepo) WithInvitationTransaction(_ context.Context, fn func(identity.InvitationTransaction) error) error {
	return fn(&transportInvitationTx{r})
}
func (r *transportInvitationRepo) FindInvitation(_ context.Context, d []byte) (identity.Invitation, error) {
	id, ok := r.digests[string(d)]
	if !ok {
		return identity.Invitation{}, identity.ErrNotFound
	}
	return r.rows[id], nil
}
func (t *transportInvitationTx) RequireOwner(context.Context, string, string) error { return nil }
func (t *transportInvitationTx) GetMembership(_ context.Context, u, h string) (string, error) {
	if role := t.r.members[h][u]; role != "" {
		return role, nil
	}
	return "", household.ErrNotFound
}
func (t *transportInvitationTx) CreateInvitation(_ context.Context, in identity.CreateInvitationInput) (identity.Invitation, bool, error) {
	key := in.HouseholdID + in.ActorID + in.CreationKey
	if id := t.r.keys[key]; id != "" {
		return t.r.rows[id], false, nil
	}
	t.r.next++
	id := fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b%02x", t.r.next)
	inv := identity.Invitation{ID: id, HouseholdID: in.HouseholdID, CreatedByUserID: in.ActorID, CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt}
	t.r.keys[key] = id
	t.r.rows[id] = inv
	t.r.digests[string(in.TokenHash)] = id
	return inv, true, nil
}
func (t *transportInvitationTx) ListInvitations(context.Context, string, string, int) ([]identity.Invitation, string, error) {
	var out []identity.Invitation
	for _, v := range t.r.rows {
		out = append(out, v)
	}
	return out, "", nil
}
func (t *transportInvitationTx) RevokeInvitation(_ context.Context, h, id string, now time.Time) error {
	v, ok := t.r.rows[id]
	if !ok || v.HouseholdID != h {
		return household.ErrNotFound
	}
	if v.AcceptedAt == nil && v.RevokedAt == nil {
		v.RevokedAt = &now
		t.r.rows[id] = v
	}
	return nil
}
func (t *transportInvitationTx) FindInvitation(_ context.Context, d []byte, _ bool) (identity.Invitation, error) {
	id, ok := t.r.digests[string(d)]
	if !ok {
		return identity.Invitation{}, identity.ErrNotFound
	}
	return t.r.rows[id], nil
}
func (t *transportInvitationTx) FindLogin(_ context.Context, l string) error {
	for _, v := range t.r.users {
		if v == l {
			return identity.ErrLoginUnavailable
		}
	}
	return identity.ErrNotFound
}
func (t *transportInvitationTx) LockUser(_ context.Context, u string) (string, error) {
	if _, ok := t.r.users[u]; !ok {
		return "", identity.ErrNotFound
	}
	return t.r.defaults[u], nil
}
func (t *transportInvitationTx) HasOtherMembership(_ context.Context, u, h string) (bool, error) {
	for hh, m := range t.r.members {
		if hh != h && m[u] != "" {
			return true, nil
		}
	}
	return false, nil
}
func (t *transportInvitationTx) CreateUser(_ context.Context, l string) (string, error) {
	t.r.next++
	id := fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b%02x", t.r.next)
	t.r.users[id] = l
	return id, nil
}
func (*transportInvitationTx) CreateCredential(context.Context, string, string) error { return nil }
func (t *transportInvitationTx) AddInvitedMember(_ context.Context, u, h string) error {
	if t.r.members[h] == nil {
		t.r.members[h] = map[string]string{}
	}
	t.r.members[h][u] = household.RoleMember
	return nil
}
func (t *transportInvitationTx) SetDefaultHousehold(_ context.Context, u, h string) error {
	t.r.defaults[u] = h
	return nil
}
func (t *transportInvitationTx) AcceptInvitation(_ context.Context, id string, n time.Time) error {
	v := t.r.rows[id]
	v.AcceptedAt = &n
	t.r.rows[id] = v
	return nil
}

type transportMembersRepo struct{ r *transportInvitationRepo }

func (t *transportMembersRepo) GetMembership(_ context.Context, u, h string) (string, error) {
	if role := t.r.members[h][u]; role != "" {
		return role, nil
	}
	return "", household.ErrNotFound
}
func (*transportMembersRepo) AddInvitedMember(context.Context, string, string) error { return nil }
func (t *transportMembersRepo) ListMembers(_ context.Context, h, c string, limit int) ([]household.Member, string, error) {
	var out []household.Member
	for u, role := range t.r.members[h] {
		if c != "" && u <= c {
			continue
		}
		out = append(out, household.Member{UserID: u, Login: t.r.users[u], Role: role})
	}
	return out, "", nil
}
func TestInvitationHTTPUsesApplicationServiceForCreateRetryAcceptAndConflict(t *testing.T) {
	repo := newTransportInvitationRepo()
	app, e := identity.NewInvitationService(repo, func() time.Time { return time.Now().UTC() })
	if e != nil {
		t.Fatal(e)
	}
	app.SetPasswordHasher(func(string) (string, error) { return "credential-hash", nil })
	r := chi.NewRouter()
	NewInvitations(app, nilAuth, func(context.Context) (string, bool) { return repo.owner, true }).Register(r)
	householdhttp.NewMembers(household.NewMembershipService(&transportMembersRepo{repo}), nilAuth, func(context.Context) (string, bool) { return repo.owner, true }).Register(r)
	hid := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	headers := map[string][]string{"Idempotency-Key": {"app-key"}}
	w := req(r, "POST", "/api/v1/households/"+hid+"/invitations", "{}", "application/json", headers)
	if w.Code != 201 {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	token, ok := created["token"].(string)
	if !ok || len(token) != 43 {
		t.Fatalf("token=%v", created["token"])
	}
	id := created["id"].(string)
	w = req(r, "POST", "/api/v1/households/"+hid+"/invitations", "{}", "application/json", headers)
	if w.Code != 200 || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("retry=%d %s", w.Code, w.Body.String())
	}
	accept := `{"token":"` + token + `","login":"new_member","password":"a sufficiently long passphrase"}`
	w = req(r, "POST", "/api/v1/auth/invitations/accept", accept, "application/json", nil)
	if w.Code != 201 {
		t.Fatalf("accept=%d %s", w.Code, w.Body.String())
	}
	var userID string
	for uid, l := range repo.users {
		if l == "new_member" {
			userID = uid
		}
	}
	if userID == "" || repo.members[hid][userID] != household.RoleMember || repo.defaults[userID] != hid || repo.rows[id].AcceptedAt == nil {
		t.Fatalf("accepted state users=%v members=%v defaults=%v invite=%+v", repo.users, repo.members, repo.defaults, repo.rows[id])
	}
	otherHouse := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	invite, e := app.CreateInvitation(context.Background(), repo.owner, otherHouse, "conflict")
	if e != nil {
		t.Fatal(e)
	}
	repo.defaults[userID] = otherHouse
	// Existing-account acceptance calls the service with this signed-in user ID.
	service2, _ := identity.NewInvitationService(repo, func() time.Time { return time.Now() })
	service2.SetPasswordHasher(func(string) (string, error) { return "unused", nil })
	r = chi.NewRouter()
	NewInvitations(service2, nilAuth, func(context.Context) (string, bool) { return userID, true }).Register(r)
	householdhttp.NewMembers(household.NewMembershipService(&transportMembersRepo{repo}), nilAuth, func(context.Context) (string, bool) { return userID, true }).Register(r)
	w = req(r, "POST", "/api/v1/invitations/accept", `{"token":"`+invite.Token+`"}`, "application/json", nil)
	assertInvitationProblem(t, w, 409, "household_conflict")
	if repo.rows[invite.Invitation.ID].AcceptedAt != nil {
		t.Fatal("conflict consumed invitation")
	}
}
