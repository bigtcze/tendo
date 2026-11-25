package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	policyAdminID  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80"
	policyTargetID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"
	policyPeerID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82"
	policyGoneID   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"
)

var (
	policyNow         = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	errPolicyInjected = errors.New("policy injected failure")
)

// policyRepo is an in-memory MCPPolicyRepository that is also the transaction
// it opens. Every operation appends its name to log, so tests assert lock
// order. A missing row is inserted in memory only by UpdateAccountPolicy, so a
// refused repair leaves the row missing, as a rollback does.
type policyRepo struct {
	log      []string
	fail     string // the operation that returns errPolicyInjected
	admins   map[string]bool
	accounts map[string]bool
	rows     map[string]MCPPolicyRow
	events   []MCPPolicyEvent
	// base holds the installation switch and the default terms.
	base MCPPolicyRecord
	// inst is the installation row (B1b2). Nil models a missing row.
	inst *MCPInstallationRow
	// grants and the revocations are the administrator grant state (B1b3).
	grants             map[string]MCPGrantRecord
	revocations        map[string]MCPRevocation
	accountRevocations map[string]MCPRevocation
}

func newPolicyRepo() *policyRepo {
	return &policyRepo{
		admins:   map[string]bool{policyAdminID: true},
		accounts: map[string]bool{policyTargetID: true},
		rows:     map[string]MCPPolicyRow{policyTargetID: policyRow(1, nil, nil, true, mcpBoth)},
		base: MCPPolicyRecord{
			InstallationFound: true, InstallationEnabled: true,
			DefaultAccountEnabled: true, DefaultMaxScopes: slices.Clone(mcpBoth),
		},
		inst:               &MCPInstallationRow{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: slices.Clone(mcpBoth), Version: 1},
		grants:             map[string]MCPGrantRecord{},
		revocations:        map[string]MCPRevocation{},
		accountRevocations: map[string]MCPRevocation{},
	}
}

// policyEventID is a uuidv7-shaped id that grows with insertion, as the
// database's uuidv7() does within one process.
func policyEventID(n int) string {
	return fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012x", n)
}

// policyRow builds a stored row. Nil administrator terms follow the default.
func policyRow(version int64, adminEnabled *bool, adminMax []string, userEnabled bool, userScopes []string) MCPPolicyRow {
	return MCPPolicyRow{
		AdminEnabled: adminEnabled, AdminMaxScopes: adminMax,
		UserEnabled: userEnabled, UserScopes: userScopes, Version: version,
	}
}

func (r *policyRepo) step(name string) error {
	r.log = append(r.log, name)
	if r.fail == name {
		return errPolicyInjected
	}
	return nil
}

func (r *policyRepo) WithMCPPolicyTransaction(_ context.Context, work func(MCPPolicyTransaction) error) error {
	r.log = append(r.log, "tx")
	return work(r)
}

func (r *policyRepo) RequireInstanceAdmin(_ context.Context, actorID string) error {
	if err := r.step("admin"); err != nil {
		return err
	}
	if !r.admins[actorID] {
		return ErrInstanceAdminRequired
	}
	return nil
}

func (r *policyRepo) LockLiveAccount(_ context.Context, userID string) error {
	if err := r.step("lockAccount"); err != nil {
		return err
	}
	if !r.accounts[userID] {
		return ErrNotFound
	}
	return nil
}

func (r *policyRepo) LockAccountPolicy(_ context.Context, userID string) (MCPPolicyRow, bool, error) {
	if err := r.step("lockPolicy"); err != nil {
		return MCPPolicyRow{}, false, err
	}
	if row, ok := r.rows[userID]; ok {
		return row, false, nil
	}
	// The database default row: the account follows the defaults, version 1.
	return policyRow(1, nil, nil, true, slices.Clone(mcpBoth)), true, nil
}

func (r *policyRepo) UpdateAccountPolicy(_ context.Context, userID string, row MCPPolicyRow, _ time.Time) error {
	if err := r.step("update"); err != nil {
		return err
	}
	r.rows[userID] = row
	return nil
}

func (r *policyRepo) AppendPolicyEvent(_ context.Context, event MCPPolicyEvent) error {
	if err := r.step("event"); err != nil {
		return err
	}
	event.ID = policyEventID(len(r.events) + 1)
	r.events = append(r.events, event)
	return nil
}

func (r *policyRepo) LockInstallationPolicy(_ context.Context) (MCPInstallationRow, error) {
	if err := r.step("lockInstallation"); err != nil {
		return MCPInstallationRow{}, err
	}
	if r.inst == nil {
		return MCPInstallationRow{}, ErrMCPInstallationMissing
	}
	return *r.inst, nil
}

func (r *policyRepo) ReadInstallationPolicy(_ context.Context) (MCPInstallationRow, error) {
	if err := r.step("readInstallation"); err != nil {
		return MCPInstallationRow{}, err
	}
	if r.inst == nil {
		return MCPInstallationRow{}, ErrMCPInstallationMissing
	}
	return *r.inst, nil
}

func (r *policyRepo) UpdateInstallationPolicy(_ context.Context, row MCPInstallationRow, _ time.Time) error {
	if err := r.step("update"); err != nil {
		return err
	}
	if r.inst == nil {
		return ErrMCPInstallationMissing
	}
	stored := row
	r.inst = &stored
	return nil
}

// ListPolicyEvents returns events with id below afterID (when set), newest
// first, at most limit of them.
func (r *policyRepo) ListPolicyEvents(_ context.Context, afterID string, limit int) ([]MCPPolicyEvent, error) {
	if err := r.step("listEvents"); err != nil {
		return nil, err
	}
	var out []MCPPolicyEvent
	for i := len(r.events) - 1; i >= 0 && len(out) < limit; i-- {
		if afterID == "" || r.events[i].ID < afterID {
			out = append(out, r.events[i])
		}
	}
	return out, nil
}

func (r *policyRepo) ReadLiveAccountPolicy(_ context.Context, userID string) (MCPPolicyRecord, int64, error) {
	if err := r.step("read"); err != nil {
		return MCPPolicyRecord{}, 0, err
	}
	if !r.accounts[userID] {
		return MCPPolicyRecord{}, 0, ErrNotFound
	}
	record := r.base
	row, ok := r.rows[userID]
	if !ok {
		return record, 0, nil
	}
	record.AccountFound = true
	record.AdminEnabled, record.AdminMaxScopes = row.AdminEnabled, row.AdminMaxScopes
	record.UserEnabled, record.UserScopes = row.UserEnabled, row.UserScopes
	return record, row.Version, nil
}

func newPolicyService(t *testing.T, repo *policyRepo, mcpEnabled bool) *MCPPolicyService {
	t.Helper()
	svc, err := NewMCPPolicyService(repo, mcpEnabled, func() time.Time { return policyNow })
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func policyAssertLog(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("log=%v want %v", got, want)
	}
}

// policyAssertScopes requires a non-nil list: an empty set is [] on the wire.
func policyAssertScopes(t *testing.T, label string, got, want []string) {
	t.Helper()
	if got == nil || !slices.Equal(got, want) {
		t.Fatalf("%s=%#v want %#v", label, got, want)
	}
}

func TestMCPPolicyAdminReadRequiresInstanceAdmin(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).AccountPolicy(context.Background(), policyPeerID, policyTargetID)
	if !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("err=%v want ErrInstanceAdminRequired", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin")
}

func TestMCPPolicyAdminReadMalformedIDIsNotFound(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", strings.ToUpper(policyTargetID), strings.ReplaceAll(policyTargetID, "-", "")} {
		repo := newPolicyRepo()
		_, err := newPolicyService(t, repo, true).AccountPolicy(context.Background(), policyAdminID, id)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%q: err=%v want ErrNotFound", id, err)
		}
		policyAssertLog(t, repo.log, "tx", "admin")
	}
}

func TestMCPPolicyAdminReadDeletedIsNotFound(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).AccountPolicy(context.Background(), policyAdminID, policyGoneID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestMCPPolicyAdminReadNilAdminColumnsAndSortedUserScopes(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(7, nil, nil, true, []string{MCPScopeWrite, MCPScopeRead})
	got, err := newPolicyService(t, repo, true).AccountPolicy(context.Background(), policyAdminID, policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AdminEnabled != nil || got.AdminMaxScopes != nil || got.Version != 7 || !got.UserEnabled {
		t.Fatalf("view=%+v", got)
	}
	policyAssertScopes(t, "userScopes", got.UserScopes, mcpBoth)
	policyAssertScopes(t, "effective", got.Effective, mcpBoth)
}

// TestMCPPolicyAdminReadEffectiveFollowsMCPFlag: the administrator view sorts
// the stored maximum; the effective set is empty while TENDO_MCP_ENABLED is off.
func TestMCPPolicyAdminReadEffectiveFollowsMCPFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    []string
	}{
		{"mcp on", true, mcpBoth},
		{"mcp off", false, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPolicyRepo()
			repo.rows[policyTargetID] = policyRow(3, mcpBool(true), []string{MCPScopeWrite, MCPScopeRead}, true, mcpBoth)
			got, err := newPolicyService(t, repo, tc.enabled).AccountPolicy(context.Background(), policyAdminID, policyTargetID)
			if err != nil {
				t.Fatal(err)
			}
			if got.AdminEnabled == nil || !*got.AdminEnabled {
				t.Fatal("administrator override lost")
			}
			policyAssertScopes(t, "adminMaxScopes", got.AdminMaxScopes, mcpBoth)
			policyAssertScopes(t, "effective", got.Effective, tc.want)
		})
	}
}

// TestMCPPolicyAdminReadMissingRowReportsRepair: a missing row reports the
// values its repair inserts, version 1, and an inactive effective set.
func TestMCPPolicyAdminReadMissingRowReportsRepair(t *testing.T) {
	repo := newPolicyRepo()
	delete(repo.rows, policyTargetID)
	got, err := newPolicyService(t, repo, true).AccountPolicy(context.Background(), policyAdminID, policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.AdminEnabled != nil || got.AdminMaxScopes != nil || !got.UserEnabled {
		t.Fatalf("view=%+v", got)
	}
	policyAssertScopes(t, "userScopes", got.UserScopes, mcpBoth)
	policyAssertScopes(t, "effective", got.Effective, []string{})
}

// TestMCPPolicyAdminUpdateLockOrder: the administrator check, then the account
// lock, then the policy row lock, then the write and its single event.
func TestMCPPolicyAdminUpdateLockOrder(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPAccountPolicyChange{Enabled: mcpBool(false), MaxScopes: mcpReadOnly}
	if _, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, change, 1); err != nil {
		t.Fatal(err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockAccount", "lockPolicy", "update", "event", "read")
}

func TestMCPPolicyAdminUpdateStaleVersionWritesNothing(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPAccountPolicyChange{Enabled: mcpBool(false)}
	_, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, change, 6)
	if !errors.Is(err, ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("err=%v want precondition failed", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockAccount", "lockPolicy")
	if len(repo.events) != 0 || repo.rows[policyTargetID].Version != 1 {
		t.Fatal("a stale write stored or audited")
	}
}

// TestMCPPolicyAdminUpdateNoOpKeepsVersion: the same values, with maxScopes in
// another order, change nothing and append nothing.
func TestMCPPolicyAdminUpdateNoOpKeepsVersion(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(4, mcpBool(false), mcpBoth, true, mcpBoth)
	change := MCPAccountPolicyChange{Enabled: mcpBool(false), MaxScopes: []string{MCPScopeWrite, MCPScopeRead}}
	got, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, change, 4)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(repo.log, "update") || len(repo.events) != 0 {
		t.Fatalf("no-op wrote: log=%v events=%d", repo.log, len(repo.events))
	}
	if got.Version != 4 {
		t.Fatalf("version=%d want 4", got.Version)
	}
}

func TestMCPPolicyAdminUpdateChangeBumpsVersionAndAudits(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPAccountPolicyChange{Enabled: mcpBool(false), MaxScopes: []string{MCPScopeWrite, MCPScopeRead}}
	got, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, change, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || repo.rows[policyTargetID].Version != 2 {
		t.Fatalf("version=%d stored=%d want 2", got.Version, repo.rows[policyTargetID].Version)
	}
	if len(repo.events) != 1 {
		t.Fatalf("events=%d want 1", len(repo.events))
	}
	ev := repo.events[0]
	if ev.Kind != MCPPolicyEventAccount || ev.ActorUserID != policyAdminID || ev.TargetUserID != policyTargetID {
		t.Fatalf("event=%+v", ev)
	}
	if ev.Enabled == nil || *ev.Enabled || !slices.Equal(ev.Scopes, mcpBoth) {
		t.Fatalf("event terms=%v %v", ev.Enabled, ev.Scopes)
	}
	if !slices.Equal(got.AdminMaxScopes, mcpBoth) || got.AdminEnabled == nil || *got.AdminEnabled {
		t.Fatalf("view=%+v", got)
	}
}

// TestMCPPolicyAdminUpdateNullTermsStoreNil: nil terms return to the default
// and are recorded as nil in the event.
func TestMCPPolicyAdminUpdateNullTermsStoreNil(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(2, mcpBool(false), mcpReadOnly, true, mcpBoth)
	if _, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, MCPAccountPolicyChange{}, 2); err != nil {
		t.Fatal(err)
	}
	stored := repo.rows[policyTargetID]
	if stored.AdminEnabled != nil || stored.AdminMaxScopes != nil || stored.Version != 3 {
		t.Fatalf("stored=%+v", stored)
	}
	ev := repo.events[0]
	if ev.Enabled != nil || ev.Scopes != nil {
		t.Fatalf("event terms=%v %#v", ev.Enabled, ev.Scopes)
	}
}

func TestMCPPolicyAdminUpdateInvalidMaxScopesBeforeRepository(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scopes []string
	}{
		{"empty", []string{}},
		{"duplicate", []string{MCPScopeRead, MCPScopeRead}},
		{"unknown", []string{"tendo:admin"}},
		{"three", []string{MCPScopeRead, MCPScopeWrite, "tendo:x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPolicyRepo()
			_, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, MCPAccountPolicyChange{MaxScopes: tc.scopes}, 1)
			policyAssertValidation(t, err, "maxScopes")
			if len(repo.log) != 0 {
				t.Fatalf("repository reached: %v", repo.log)
			}
		})
	}
}

func TestMCPPolicyAdminUpdateNilMaxScopesAccepted(t *testing.T) {
	repo := newPolicyRepo()
	if _, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, MCPAccountPolicyChange{Enabled: mcpBool(true)}, 1); err != nil {
		t.Fatalf("nil maxScopes: %v", err)
	}
}

// TestMCPPolicyAdminUpdateRepairWritesEvenAtDefaults: a missing row is always
// written and audited once, even when the values equal the defaults.
func TestMCPPolicyAdminUpdateRepairWritesEvenAtDefaults(t *testing.T) {
	repo := newPolicyRepo()
	delete(repo.rows, policyTargetID)
	got, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, MCPAccountPolicyChange{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.events) != 1 || !slices.Contains(repo.log, "update") {
		t.Fatalf("repair: log=%v events=%d", repo.log, len(repo.events))
	}
	if got.Version != 2 {
		t.Fatalf("version=%d want 2", got.Version)
	}
}

func TestMCPPolicyAdminUpdateNotAdminWritesNothing(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyPeerID, policyTargetID, MCPAccountPolicyChange{Enabled: mcpBool(false)}, 1)
	if !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("err=%v", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin")
}

func TestMCPPolicyAdminUpdateRepositoryErrorAppendsNoEvent(t *testing.T) {
	repo := newPolicyRepo()
	repo.fail = "update"
	_, err := newPolicyService(t, repo, true).UpdateAccountPolicy(context.Background(), policyAdminID, policyTargetID, MCPAccountPolicyChange{Enabled: mcpBool(false)}, 1)
	if !errors.Is(err, errPolicyInjected) {
		t.Fatalf("err=%v want the injected error", err)
	}
	if len(repo.events) != 0 {
		t.Fatal("event appended after a failed update")
	}
}

// policyAssertValidation requires the 422 validation error on field.
func policyAssertValidation(t *testing.T, err error, field string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) || v.Field != field || v.Code != "invalid_value" {
		t.Fatalf("err=%v want ValidationError{%s, invalid_value}", err, field)
	}
}

func TestMCPPolicySettingsDeletedIsUnauthenticated(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).Settings(context.Background(), policyGoneID)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err=%v want ErrUnauthenticated", err)
	}
}

// TestMCPPolicySettingsMaximumIgnoresOwnTerms: a user who switched themselves
// off still sees the maximum the operator and administrator allow.
func TestMCPPolicySettingsMaximumIgnoresOwnTerms(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(5, nil, nil, false, mcpReadOnly)
	got, err := newPolicyService(t, repo, true).Settings(context.Background(), policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Version != 5 {
		t.Fatalf("view=%+v", got)
	}
	policyAssertScopes(t, "scopes", got.Scopes, mcpReadOnly)
	policyAssertScopes(t, "maximum", got.Maximum, mcpBoth)
	policyAssertScopes(t, "effective", got.Effective, []string{})
}

func TestMCPPolicySettingsEffectiveIsMaximumIntersectUser(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(2, mcpBool(true), mcpBoth, true, mcpReadOnly)
	got, err := newPolicyService(t, repo, true).Settings(context.Background(), policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	policyAssertScopes(t, "maximum", got.Maximum, mcpBoth)
	policyAssertScopes(t, "effective", got.Effective, mcpReadOnly)
}

func TestMCPPolicySettingsAdminDisabledIsEmptyEffective(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(2, mcpBool(false), mcpBoth, true, mcpBoth)
	got, err := newPolicyService(t, repo, true).Settings(context.Background(), policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	policyAssertScopes(t, "maximum", got.Maximum, []string{})
	policyAssertScopes(t, "effective", got.Effective, []string{})
}

func TestMCPPolicySettingsMCPOffIsEmptyBoth(t *testing.T) {
	repo := newPolicyRepo()
	got, err := newPolicyService(t, repo, false).Settings(context.Background(), policyTargetID)
	if err != nil {
		t.Fatal(err)
	}
	policyAssertScopes(t, "maximum", got.Maximum, []string{})
	policyAssertScopes(t, "effective", got.Effective, []string{})
}

func TestMCPPolicySettingsUpdateDeletedIsUnauthenticated(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPSettingsChange{Enabled: true, Scopes: mcpReadOnly}
	_, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyGoneID, change, 1)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err=%v want ErrUnauthenticated", err)
	}
	policyAssertLog(t, repo.log, "tx", "lockAccount")
}

func TestMCPPolicySettingsUpdateInvalidScopesBeforeRepository(t *testing.T) {
	for _, scopes := range [][]string{nil, {}, {MCPScopeRead, MCPScopeRead}, {"tendo:admin"}, {MCPScopeRead, MCPScopeWrite, MCPScopeRead}} {
		repo := newPolicyRepo()
		_, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: true, Scopes: scopes}, 1)
		policyAssertValidation(t, err, "scopes")
		if len(repo.log) != 0 {
			t.Fatalf("scopes %v reached the repository: %v", scopes, repo.log)
		}
	}
}

func TestMCPPolicySettingsUpdateNoOpWritesNoEvent(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(3, nil, nil, true, []string{MCPScopeWrite, MCPScopeRead})
	got, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: true, Scopes: mcpBoth}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(repo.log, "update") || len(repo.events) != 0 || got.Version != 3 {
		t.Fatalf("no-op: log=%v events=%d version=%d", repo.log, len(repo.events), got.Version)
	}
}

func TestMCPPolicySettingsUpdateChangeAuditsUserEvent(t *testing.T) {
	repo := newPolicyRepo()
	got, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: false, Scopes: []string{MCPScopeWrite, MCPScopeRead}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	policyAssertLog(t, repo.log, "tx", "lockAccount", "lockPolicy", "update", "event", "read")
	if len(repo.events) != 1 {
		t.Fatalf("events=%d want 1", len(repo.events))
	}
	ev := repo.events[0]
	if ev.Kind != MCPPolicyEventUser || ev.ActorUserID != policyTargetID || ev.TargetUserID != policyTargetID {
		t.Fatalf("event=%+v", ev)
	}
	if ev.Enabled == nil || *ev.Enabled || !slices.Equal(ev.Scopes, mcpBoth) {
		t.Fatalf("event terms=%v %v", ev.Enabled, ev.Scopes)
	}
	if got.Version != 2 || got.Enabled || !slices.Equal(got.Scopes, mcpBoth) {
		t.Fatalf("view=%+v", got)
	}
}

func TestMCPPolicySettingsUpdateStaleVersionWritesNothing(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: false, Scopes: mcpReadOnly}, 9)
	if !errors.Is(err, ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("err=%v want precondition failed", err)
	}
	if slices.Contains(repo.log, "update") || len(repo.events) != 0 {
		t.Fatalf("stale write: log=%v", repo.log)
	}
}

// TestMCPPolicySettingsUserCannotWiden: the owner's write stores both scopes,
// but the effective set stays inside the administrator's read-only maximum.
func TestMCPPolicySettingsUserCannotWiden(t *testing.T) {
	repo := newPolicyRepo()
	repo.rows[policyTargetID] = policyRow(1, nil, mcpReadOnly, true, mcpReadOnly)
	got, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: true, Scopes: mcpBoth}, 1)
	if err != nil {
		t.Fatal(err)
	}
	policyAssertScopes(t, "scopes", got.Scopes, mcpBoth)
	policyAssertScopes(t, "maximum", got.Maximum, mcpReadOnly)
	policyAssertScopes(t, "effective", got.Effective, mcpReadOnly)
}

func TestMCPPolicySettingsUpdateRepositoryErrorAppendsNoEvent(t *testing.T) {
	repo := newPolicyRepo()
	repo.fail = "update"
	_, err := newPolicyService(t, repo, true).UpdateSettings(context.Background(), policyTargetID, MCPSettingsChange{Enabled: false, Scopes: mcpReadOnly}, 1)
	if !errors.Is(err, errPolicyInjected) {
		t.Fatalf("err=%v want the injected error", err)
	}
	if len(repo.events) != 0 {
		t.Fatal("event appended after a failed update")
	}
}
