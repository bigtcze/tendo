package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLinkedUserPatchPersonConstraintAndClear(t *testing.T) {
	f := newFixture(true)
	created := f.doAs("POST", base, `{"name":"Linked child","type":"person","linkedUserId":"`+callerID+`"}`, map[string]string{"Idempotency-Key": "linked-person"}, callerID)
	if created.Code != 201 || !strings.Contains(created.Body.String(), `"linkedUserId":"`+callerID+`"`) {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body)
	}
	var body struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	path := base + "/" + body.Id
	away := f.doAs("PATCH", path, `{"type":"vehicle"}`, map[string]string{"If-Match": `"1"`}, callerID)
	if away.Code != 422 || !strings.Contains(away.Body.String(), `"field":"linkedUserId"`) || !strings.Contains(away.Body.String(), `"code":"requires_person"`) {
		t.Fatalf("type change status=%d body=%s", away.Code, away.Body)
	}
	clearAndAway := f.doAs("PATCH", path, `{"type":"vehicle","linkedUserId":null}`, map[string]string{"If-Match": `"1"`}, callerID)
	if clearAndAway.Code != 200 || !strings.Contains(clearAndAway.Body.String(), `"type":"vehicle"`) || !strings.Contains(clearAndAway.Body.String(), `"linkedUserId":null`) {
		t.Fatalf("clear and type change status=%d body=%s", clearAndAway.Code, clearAndAway.Body)
	}
}

func TestLinkedUserWrongJSONTypesAre400AndDoNotMutate(t *testing.T) {
	for _, raw := range []string{`7`, `true`, `{"id":"x"}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			f := newFixture(true)
			before := f.repo.Count()
			post := f.doAs("POST", base, `{"name":"Wrong type","type":"person","linkedUserId":`+raw+`}`, map[string]string{"Idempotency-Key": "bad-" + raw}, callerID)
			if post.Code != 400 || !strings.Contains(post.Body.String(), `"code":"invalid_request"`) || f.repo.Count() != before {
				t.Fatalf("POST=%d %s count=%d", post.Code, post.Body, f.repo.Count())
			}
			created := f.doAs("POST", base, `{"name":"Valid linked","type":"person","linkedUserId":"`+callerID+`"}`, map[string]string{"Idempotency-Key": "valid-" + raw}, callerID)
			if created.Code != 201 {
				t.Fatalf("seed POST=%d %s", created.Code, created.Body)
			}
			var body struct {
				Id string `json:"id"`
			}
			if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			patch := f.doAs("PATCH", base+"/"+body.Id, `{"linkedUserId":`+raw+`}`, map[string]string{"If-Match": `"1"`}, callerID)
			row, _ := f.repo.Row(body.Id)
			if patch.Code != 400 || !strings.Contains(patch.Body.String(), `"code":"invalid_request"`) || row.Version != 1 || row.LinkedUserID == nil || *row.LinkedUserID != callerID {
				t.Fatalf("PATCH=%d %s row=%+v", patch.Code, patch.Body, row)
			}
		})
	}
}

func TestCreateOmittedAndNullLinkedUserReplayEquivalently(t *testing.T) {
	f := newFixture(true)
	key := "omitted-null-equivalent"
	before := f.repo.Count()
	first := f.doAs("POST", base, `{"name":"Optional","type":"person"}`, map[string]string{"Idempotency-Key": key}, callerID)
	second := f.doAs("POST", base, `{"name":"Optional","type":"person","linkedUserId":null}`, map[string]string{"Idempotency-Key": key}, callerID)
	if first.Code != 201 || second.Code != 201 || !jsonEqual(t, first.Body.String(), second.Body.String()) || f.repo.Count() != before+1 {
		t.Fatalf("omitted=%d %s null=%d %s count=%d", first.Code, first.Body, second.Code, second.Body, f.repo.Count())
	}
}
