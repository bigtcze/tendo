package httpapi

import (
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

func createRequest(values map[string]any) (string, subject.Type, *string) {
	var linked *string
	if value, present := values["linkedUserId"]; present && value != nil {
		text := value.(string)
		linked = &text
	}
	return values["name"].(string), subject.Type(values["type"].(string)), linked
}

func patchRequest(values map[string]any) subject.Patch {
	var patch subject.Patch
	if value, present := values["name"]; present {
		text := value.(string)
		patch.Name = &text
	}
	if value, present := values["type"]; present {
		typ := subject.Type(value.(string))
		patch.Type = &typ
	}
	if value, present := values["archived"]; present {
		archived := value.(bool)
		patch.Archived = &archived
	}
	if value, present := values["linkedUserId"]; present {
		patch.LinkedUserIDSet = true
		if value != nil {
			linked := value.(string)
			patch.LinkedUserID = &linked
		}
	}
	return patch
}

func readSubjectIDs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	values, present := r.URL.Query()["id"]
	if !present {
		return nil, true
	}
	if len(values) == 0 || len(values) > 100 {
		httpx.QueryProblemResponse(w, "id")
		return nil, false
	}
	for _, value := range values {
		if value == "" {
			httpx.QueryProblemResponse(w, "id")
			return nil, false
		}
	}
	return values, true
}
