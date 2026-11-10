package postgres

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// TestInvitationListHidesExpiredBeforePaging seeds interleaved invitation states, including whole
// runs of expired rows in the middle and at the end, and proves the includeExpired=false walk
// returns exactly the non-expired rows in ID order with full pages and no trailing cursor.
func TestInvitationListHidesExpiredBeforePaging(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	created := now.Add(-10 * 24 * time.Hour)
	// The kind of each seeded row in ID order. "boundary" expires exactly at now, which is expired.
	kinds := []string{
		"pending", "expired", "expired", "expired", "expired", "expired", "accepted", "boundary",
		"revoked", "pending", "expired", "acceptedLate", "revokedLate", "pending", "expired", "expired", "expired",
	}
	var want, all []string
	for n, kind := range kinds {
		id := fmt.Sprintf("0198a3f3-0000-7000-8000-%012d", n+1)
		expires, accepted, revoked := now.Add(time.Hour), "NULL", "NULL"
		switch kind {
		case "expired":
			expires = now.Add(-time.Hour)
		case "boundary":
			expires = now
		case "accepted":
			accepted = "created_at + interval '1 hour'"
		case "revoked":
			revoked = "created_at + interval '1 hour'"
		case "acceptedLate":
			// Accepted before its expiry, which has since passed: history, not an expired invite.
			expires, accepted = now.Add(-time.Hour), "created_at + interval '1 hour'"
		case "revokedLate":
			expires, revoked = now.Add(-time.Hour), "created_at + interval '1 hour'"
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(id,household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
			VALUES($1::uuid,$2::uuid,$3::uuid,$4,decode(lpad(to_hex($5::int),64,'0'),'hex'),$6,$7)`,
			id, house, owner, "hide-expired-"+kind+fmt.Sprint(n), n+1, created, expires); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE household_invitations SET accepted_at=`+accepted+`, revoked_at=`+revoked+` WHERE id=$1::uuid`, id); err != nil {
			t.Fatal(err)
		}
		all = append(all, id)
		if kind != "expired" && kind != "boundary" {
			want = append(want, id)
		}
	}
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	walk := func(limit int, includeExpired bool) ([]string, []identity.Invitation, []int) {
		t.Helper()
		var ids []string
		var rows []identity.Invitation
		var sizes []int
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > len(kinds) {
				t.Fatal("invitation walk did not terminate")
			}
			page, next, err := service.ListInvitations(ctx, owner, house, limit, cursor, includeExpired)
			if err != nil {
				t.Fatal(err)
			}
			sizes = append(sizes, len(page))
			for _, row := range page {
				ids = append(ids, row.ID)
				rows = append(rows, row)
			}
			if next == "" {
				return ids, rows, sizes
			}
			cursor = next
		}
	}
	for _, limit := range []int{1, 2, 3, 4, 100} {
		ids, rows, sizes := walk(limit, false)
		if !slices.Equal(ids, want) {
			t.Fatalf("limit=%d current walk=%v want=%v", limit, ids, want)
		}
		for i, size := range sizes {
			// Every page but the last is full, and the last is never empty: hidden rows never
			// leave a blank page or a Next that leads nowhere.
			if (i < len(sizes)-1 && size != limit) || size == 0 {
				t.Fatalf("limit=%d page sizes=%v", limit, sizes)
			}
		}
		for _, row := range rows {
			if row.Status == "expired" {
				t.Fatalf("limit=%d returned expired %s", limit, row.ID)
			}
		}
		statuses := map[string]string{}
		for _, row := range rows {
			statuses[row.ID] = row.Status
		}
		if statuses[all[11]] != "accepted" || statuses[all[12]] != "revoked" || statuses[all[0]] != "pending" {
			t.Fatalf("limit=%d terminal history statuses=%v", limit, statuses)
		}
		full, fullRows, _ := walk(limit, true)
		if !slices.Equal(full, all) {
			t.Fatalf("limit=%d full walk=%v want=%v", limit, full, all)
		}
		expired := 0
		for _, row := range fullRows {
			if row.Status == "expired" {
				expired++
			}
		}
		if expired != len(all)-len(want) {
			t.Fatalf("limit=%d full walk expired=%d", limit, expired)
		}
	}
	// A cursor names only the last returned ID, so it continues in either mode.
	first, next, err := service.ListInvitations(ctx, owner, house, 1, "", false)
	if err != nil || len(first) != 1 || first[0].ID != all[0] || next == "" {
		t.Fatalf("first=%v next=%q err=%v", first, next, err)
	}
	afterFull, _, err := service.ListInvitations(ctx, owner, house, 2, next, true)
	if err != nil || len(afterFull) != 2 || afterFull[0].ID != all[1] || afterFull[1].ID != all[2] {
		t.Fatalf("full continuation=%v err=%v", afterFull, err)
	}
	// Hiding is read-only: every stored row and its audit columns survive.
	var stored, unused int
	if err := admin.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= $2)
		FROM household_invitations WHERE household_id=$1::uuid`, house, now).Scan(&stored, &unused); err != nil || stored != len(all) || unused != len(all)-len(want) {
		t.Fatalf("stored=%d expired=%d err=%v", stored, unused, err)
	}
}

// TestInvitationListHidesExpiredStillRequiresAdmin keeps the admin-only boundary for the filtered list.
func TestInvitationListHidesExpiredStillRequiresAdmin(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	var member string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('hide_expired_member') RETURNING id::text`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, house); err != nil {
		t.Fatal(err)
	}
	service, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	if _, err := service.CreateInvitation(ctx, owner, house, "hide-expired-admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ListInvitations(ctx, member, house, 10, "", false); err == nil {
		t.Fatal("member listed current invitations")
	}
	var outsider, other string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('hide_expired_outsider') RETURNING id::text`).Scan(&outsider); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('hide expired other','UTC') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, outsider, other); err != nil {
		t.Fatal(err)
	}
	if rows, _, err := service.ListInvitations(ctx, outsider, house, 10, "", false); err == nil || len(rows) != 0 {
		t.Fatalf("foreign admin rows=%v err=%v", rows, err)
	}
}
