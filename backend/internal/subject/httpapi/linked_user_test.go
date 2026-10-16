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
	away := f.doAs("PATCH", path, `{"type":"vehicle"}`, map[string]string{"If-Match": "\"1\""}, callerID)
	if away.Code != 422 || !strings.Contains(away.Body.String(), `"field":"linkedUserId"`) || !strings.Contains(away.Body.String(), `"code":"requires_person"`) {
		t.Fatalf("type change status=%d body=%s", away.Code, away.Body)
	}
	clearAndAway := f.doAs("PATCH", path, `{"type":"vehicle","linkedUserId":null}`, map[string]string{"If-Match": "\"1\""}, callerID)
	if clearAndAway.Code != 200 || !strings.Contains(clearAndAway.Body.String(), `"type":"vehicle"`) || !strings.Contains(clearAndAway.Body.String(), `"linkedUserId":null`) {
		t.Fatalf("clear and type change status=%d body=%s", clearAndAway.Code, clearAndAway.Body)
	}
}
