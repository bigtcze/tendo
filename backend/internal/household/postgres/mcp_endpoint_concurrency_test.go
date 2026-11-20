package postgres

import (
	"fmt"
	"sync"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
)

// TestMCPEndpointConcurrentCallersSeeOnlyTheirOwnHouseholds interleaves many
// tools/call requests from two users with disjoint households. Each response
// must list only that user's household IDs.
func TestMCPEndpointConcurrentCallersSeeOnlyTheirOwnHouseholds(t *testing.T) {
	f := newMCPEndpointFixture(t)
	type caller struct {
		token string
		want  map[string]bool
	}
	callers := make([]caller, 2)
	for i := range callers {
		user := f.newUser()
		f.password(user)
		want := map[string]bool{}
		for j := 0; j < 2; j++ {
			id := f.newHousehold(fmt.Sprintf("caller %d house %d", i, j))
			f.join(user, id, household.RoleMember)
			want[id] = true
		}
		callers[i] = caller{token: f.mint(t, user, identityapp.MCPScopeRead).AccessToken, want: want}
	}

	const perCaller = 40
	var wg sync.WaitGroup
	errs := make(chan error, perCaller*len(callers))
	for n := 0; n < perCaller; n++ {
		for _, c := range callers {
			wg.Add(1)
			go func(c caller, n int) {
				defer wg.Done()
				rows, err := mcpEndpointParseList(f.post(c.token, mcpEndpointListCall(n)))
				if err != nil {
					errs <- err
					return
				}
				if len(rows) != len(c.want) {
					errs <- fmt.Errorf("got %d households, want %d", len(rows), len(c.want))
					return
				}
				for _, row := range rows {
					if !c.want[fmt.Sprint(row["id"])] {
						errs <- fmt.Errorf("foreign household leaked: %v", row)
						return
					}
				}
			}(c, n)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
