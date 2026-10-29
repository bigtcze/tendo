package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

func TestInvitationKeysetWalkPostgres(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	const count = 1007
	_, err := admin.Exec(ctx, `INSERT INTO household_invitations(id,household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		SELECT ('0198a3f2-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,$1::uuid,$2::uuid,'bulk-'||n::text,
		       decode(lpad(to_hex(n),64,'0'),'hex'),TIMESTAMPTZ '2026-01-01 00:00:00+00',TIMESTAMPTZ '2026-02-01 00:00:00+00'
		FROM generate_series(1,$3)n`, house, owner, count)
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	rows, err := admin.Query(ctx, `SELECT id::text FROM household_invitations WHERE household_id=$1::uuid ORDER BY id`, house)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) != count {
		t.Fatalf("fixture rows=%d want=%d", len(want), count)
	}
	for _, limit := range []int{100, 7} {
		var got []string
		cursor := ""
		for pages := 0; pages < 200; pages++ {
			invitations, next, err := service.ListInvitations(ctx, owner, house, limit, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, invitation := range invitations {
				got = append(got, invitation.ID)
			}
			if next == "" {
				break
			}
			cursor = next
			if pages == 199 {
				t.Fatal("invitation cursor walk did not terminate")
			}
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("limit=%d got=%d invitations, want=%d", limit, len(got), len(want))
		}
	}
}
