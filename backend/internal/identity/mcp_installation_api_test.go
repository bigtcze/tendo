package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// Slice B1b2 of #457 (ADR 0023 "Slice B1b2"): the installation switch the MCP
// gate reads, the installation policy API and the policy audit list.

// TestMCPAvailableReadsInstallationSwitch: TENDO_MCP_ENABLED off never touches
// the database. With it on, the installation switch decides, and a read failure
// or a missing installation row is an error the caller fails closed on.
func TestMCPAvailableReadsInstallationSwitch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		switchOn  bool
		switchErr error
		want      bool
		wantErr   bool
		wantReads int
		// bare is a repository without MCPInstallationStore: the service has no
		// switch to read and must fail closed rather than default to on.
		bare bool
	}{
		{name: "env off never reads", enabled: false, switchOn: true, want: false, wantReads: 0},
		{name: "env on installation on", enabled: true, switchOn: true, want: true, wantReads: 1},
		{name: "env on installation off", enabled: true, switchOn: false, want: false, wantReads: 1},
		{name: "read failure fails closed", enabled: true, switchErr: errMCPInjected, want: false, wantErr: true, wantReads: 1},
		{name: "missing row fails closed", enabled: true, switchErr: ErrMCPInstallationMissing, want: false, wantErr: true, wantReads: 1},
		{name: "no installation store fails closed", enabled: true, want: false, wantErr: true, bare: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMCPFake(mcpTestStart)
			f.policy.InstallationEnabled = tc.switchOn
			f.switchErr = tc.switchErr
			var repo MCPRepository = f
			if tc.bare {
				repo = &bareMCPRepository{}
			}
			svc, err := NewMCPService(repo, MCPOptions{Enabled: tc.enabled, Resource: mcpTestResource}, f.clock)
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.Available(context.Background())
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("Available()=%v err=%v; want %v err=%v", got, err, tc.want, tc.wantErr)
			}
			if !tc.bare && f.switchReads != tc.wantReads {
				t.Fatalf("switch reads=%d want %d", f.switchReads, tc.wantReads)
			}
		})
	}
}

func TestMCPInstallationReadRequiresInstanceAdmin(t *testing.T) {
	repo := newPolicyRepo()
	_, err := newPolicyService(t, repo, true).InstallationPolicy(context.Background(), policyPeerID)
	if !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("err=%v want ErrInstanceAdminRequired", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin")
}

func TestMCPInstallationReadReportsOperatorSwitch(t *testing.T) {
	repo := newPolicyRepo()
	out, err := newPolicyService(t, repo, false).InstallationPolicy(context.Background(), policyAdminID)
	if err != nil {
		t.Fatal(err)
	}
	if out.OperatorEnabled || !out.Enabled || !out.DefaultAccountEnabled || !slices.Equal(out.DefaultMaxScopes, mcpBoth) || out.Version != 1 {
		t.Fatalf("view=%+v", out)
	}
}

// TestMCPInstallationPutNoOpWritesNothing: an unchanged PUT keeps the version
// and appends no event.
func TestMCPInstallationPutNoOpWritesNothing(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: []string{"tendo:write", "tendo:read"}}
	out, err := newPolicyService(t, repo, true).UpdateInstallationPolicy(context.Background(), policyAdminID, change, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 1 {
		t.Fatalf("version=%d want 1", out.Version)
	}
	if len(repo.events) != 0 {
		t.Fatalf("no-op appended %d events", len(repo.events))
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockInstallation")
}

// TestMCPInstallationPutChangeAppendsOneEvent: a change bumps the version by
// one, updates the row and appends exactly one installation event whose
// target is absent.
func TestMCPInstallationPutChangeAppendsOneEvent(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPInstallationChange{Enabled: false, DefaultAccountEnabled: true, DefaultMaxScopes: []string{MCPScopeRead}}
	out, err := newPolicyService(t, repo, true).UpdateInstallationPolicy(context.Background(), policyAdminID, change, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Enabled || out.Version != 2 || !slices.Equal(out.DefaultMaxScopes, []string{MCPScopeRead}) {
		t.Fatalf("view=%+v", out)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockInstallation", "update", "event")
	if len(repo.events) != 1 {
		t.Fatalf("events=%d want 1", len(repo.events))
	}
	e := repo.events[0]
	if e.Kind != MCPPolicyEventInstallation || e.ActorUserID != policyAdminID || e.TargetUserID != "" ||
		e.Enabled == nil || *e.Enabled || e.DefaultAccountEnabled == nil || !*e.DefaultAccountEnabled ||
		!slices.Equal(e.Scopes, []string{MCPScopeRead}) || !e.OccurredAt.Equal(policyNow) {
		t.Fatalf("event=%+v", e)
	}
}

func TestMCPInstallationPutPreconditionFailsBeforeWrite(t *testing.T) {
	repo := newPolicyRepo()
	change := MCPInstallationChange{Enabled: false, DefaultAccountEnabled: true, DefaultMaxScopes: mcpBoth}
	_, err := newPolicyService(t, repo, true).UpdateInstallationPolicy(context.Background(), policyAdminID, change, 2)
	if !errors.Is(err, ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("err=%v want precondition failed", err)
	}
	policyAssertLog(t, repo.log, "tx", "admin", "lockInstallation")
	if len(repo.events) != 0 {
		t.Fatal("a stale If-Match appended an event")
	}
}

// TestMCPInstallationMissingRowIsNeverDefaulted: a missing installation row is
// an error on read and write. Nothing is fabricated and nothing is appended.
func TestMCPInstallationMissingRowIsNeverDefaulted(t *testing.T) {
	repo := newPolicyRepo()
	repo.inst = nil
	svc := newPolicyService(t, repo, true)
	if _, err := svc.InstallationPolicy(context.Background(), policyAdminID); !errors.Is(err, ErrMCPInstallationMissing) {
		t.Fatalf("read err=%v want ErrMCPInstallationMissing", err)
	}
	change := MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: mcpBoth}
	if _, err := svc.UpdateInstallationPolicy(context.Background(), policyAdminID, change, 1); !errors.Is(err, ErrMCPInstallationMissing) {
		t.Fatalf("write err=%v want ErrMCPInstallationMissing", err)
	}
	if len(repo.events) != 0 || repo.inst != nil {
		t.Fatal("a missing installation row was fabricated or audited")
	}
}

func TestMCPInstallationPutRejectsNonCanonicalScopesBeforeTransaction(t *testing.T) {
	for _, scopes := range [][]string{nil, {}, {"tendo:admin"}, {MCPScopeRead, MCPScopeRead}, {MCPScopeRead, MCPScopeWrite, MCPScopeRead}} {
		repo := newPolicyRepo()
		change := MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: scopes}
		_, err := newPolicyService(t, repo, true).UpdateInstallationPolicy(context.Background(), policyAdminID, change, 1)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != "defaultMaxScopes" {
			t.Fatalf("%v: err=%v want a defaultMaxScopes validation error", scopes, err)
		}
		if len(repo.log) != 0 {
			t.Fatalf("%v: a refused body opened a transaction: %v", scopes, repo.log)
		}
	}
}

// TestMCPPolicyEventsAdminOnlyAndCursorBeforeSQL: a non-administrator learns
// nothing; an invalid cursor is refused before any transaction or SQL.
func TestMCPPolicyEventsAdminOnlyAndCursorBeforeSQL(t *testing.T) {
	repo := newPolicyRepo()
	if _, err := newPolicyService(t, repo, true).PolicyEvents(context.Background(), policyPeerID, 10, ""); !errors.Is(err, ErrInstanceAdminRequired) {
		t.Fatalf("peer err=%v want ErrInstanceAdminRequired", err)
	}
	repo = newPolicyRepo()
	_, err := newPolicyService(t, repo, true).PolicyEvents(context.Background(), policyAdminID, 10, "p1bogus")
	if !errors.Is(err, ErrMCPInvalidCursor) {
		t.Fatalf("err=%v want ErrMCPInvalidCursor", err)
	}
	if len(repo.log) != 0 {
		t.Fatalf("an invalid cursor reached the database: %v", repo.log)
	}
}

// TestMCPPolicyEventsNewestFirstPaged: all three levels are listed newest
// first, and the opaque cursor walks to the end without repeats.
func TestMCPPolicyEventsNewestFirstPaged(t *testing.T) {
	repo := newPolicyRepo()
	svc := newPolicyService(t, repo, true)
	ctx := context.Background()
	if _, err := svc.UpdateInstallationPolicy(ctx, policyAdminID, MCPInstallationChange{Enabled: false, DefaultAccountEnabled: true, DefaultMaxScopes: mcpBoth}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAccountPolicy(ctx, policyAdminID, policyTargetID, MCPAccountPolicyChange{Enabled: boolPtr(false)}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateInstallationPolicy(ctx, policyAdminID, MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: mcpBoth}, 2); err != nil {
		t.Fatal(err)
	}
	first, err := svc.PolicyEvents(ctx, policyAdminID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	if first.Items[0].Kind != MCPPolicyEventInstallation || first.Items[1].Kind != MCPPolicyEventAccount {
		t.Fatalf("order=%v,%v want installation then account", first.Items[0].Kind, first.Items[1].Kind)
	}
	second, err := svc.PolicyEvents(ctx, policyAdminID, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Kind != MCPPolicyEventInstallation {
		t.Fatalf("second page=%+v", second)
	}
	if second.Items[0].ID == first.Items[0].ID || second.Items[0].ID >= first.Items[1].ID {
		t.Fatal("the second page repeated or outran the first")
	}
}

func TestMCPPolicyEventCursorIsExactlyOneForm(t *testing.T) {
	id := policyTargetID
	cursor, err := encodeMCPPolicyEventCursor(id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeMCPPolicyEventCursor(cursor)
	if err != nil || got != id {
		t.Fatalf("round trip=%q err=%v", got, err)
	}
	if got, err := decodeMCPPolicyEventCursor(""); err != nil || got != "" {
		t.Fatalf("empty cursor=%q err=%v want first page", got, err)
	}
	// A grant cursor is a different prefix and must not page the audit list.
	grant, err := encodeMCPGrantCursor(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{grant, cursor[:len(cursor)-1], cursor + "A", "p1" + "!!!!!!!!!!!!!!!!!!!!!!"} {
		if _, err := decodeMCPPolicyEventCursor(bad); !errors.Is(err, ErrMCPInvalidCursor) {
			t.Fatalf("%q: err=%v want ErrMCPInvalidCursor", bad, err)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
