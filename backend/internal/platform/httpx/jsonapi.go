package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file holds the transport helpers shared by household-scoped resource
// handlers: strict JSON object decoding, problem+json responses, If-Match
// parsing, ETag formatting, and list query parsing. They carry no domain rules.

var strongETag = regexp.MustCompile(`^"[1-9][0-9]*"$`)

// Kind is the JSON type accepted for an object key.
type Kind int

const (
	// KindString accepts a JSON string; null is rejected.
	KindString Kind = iota
	// KindBool accepts a JSON boolean; null is rejected.
	KindBool
	// KindNullableString accepts a JSON string or null. A null value is stored
	// as a nil entry so callers can tell it from an omitted key.
	KindNullableString
	KindInteger
	KindNullableObject
)

// Field describes one accepted object key.
type Field struct {
	Kind     Kind
	Required bool
	Fields   map[string]Field
}

// ETag formats a resource version as a strong, quoted decimal entity tag.
func ETag(version int64) string { return `"` + strconv.FormatInt(version, 10) + `"` }

// ParseIfMatch reads the single strong If-Match entity tag. A missing header
// writes 428; a malformed, weak, wildcard, repeated or listed value writes 412.
func ParseIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	matches := r.Header.Values("If-Match")
	if len(matches) == 0 {
		ProblemResponse(w, http.StatusPreconditionRequired, "precondition_required")
		return 0, false
	}
	if len(matches) != 1 || !strongETag.MatchString(matches[0]) {
		ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
		return 0, false
	}
	expected, err := strconv.ParseInt(strings.Trim(matches[0], `"`), 10, 64)
	if err != nil {
		ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
		return 0, false
	}
	return expected, true
}

// ListParams are the parsed common list query parameters. Limit 0 means the
// parameter was absent.
type ListParams struct {
	Limit    int
	Cursor   string
	Archived bool
	Done     bool
}

type ListFilter int

const (
	ListFilterArchived ListFilter = iota + 1
	ListFilterDone
	ListFilterArchivedDone
)

// ParseListParamsFor parses common pagination and only the filters enabled for this endpoint.
func ParseListParamsFor(w http.ResponseWriter, r *http.Request, maxLimit int, filter ListFilter) (ListParams, bool) {
	return parseListParams(w, r, maxLimit, filter)
}

// ParseListParams parses limit, cursor and archived. Endpoint-specific filters
// are enabled with ParseListParamsFor. On failure
// it writes the invalid_query problem naming the first offender.
func ParseListParams(w http.ResponseWriter, r *http.Request, maxLimit int) (ListParams, bool) {
	return parseListParams(w, r, maxLimit, ListFilterArchived)
}
func parseListParams(w http.ResponseWriter, r *http.Request, maxLimit int, filter ListFilter) (ListParams, bool) {
	var p ListParams
	values := r.URL.Query()
	if v, present := values["limit"]; present {
		n, ok := parseLimit(v, maxLimit)
		if !ok {
			QueryProblemResponse(w, "limit")
			return p, false
		}
		p.Limit = n
	}
	if v, present := values["cursor"]; present {
		if len(v) != 1 || v[0] == "" {
			QueryProblemResponse(w, "cursor")
			return p, false
		}
		p.Cursor = v[0]
	}
	if filter == ListFilterArchived || filter == ListFilterArchivedDone {
		if v, present := values["archived"]; present {
			if len(v) != 1 || (v[0] != "true" && v[0] != "false") {
				QueryProblemResponse(w, "archived")
				return p, false
			}
			p.Archived = v[0] == "true"
		}
	}
	if filter == ListFilterDone || filter == ListFilterArchivedDone {
		if v, present := values["done"]; present {
			if len(v) != 1 || (v[0] != "true" && v[0] != "false") {
				QueryProblemResponse(w, "done")
				return p, false
			}
			p.Done = v[0] == "true"
		}
	}
	return p, true
}

func parseLimit(values []string, maxLimit int) (int, bool) {
	if len(values) != 1 || values[0] == "" || len(values[0]) > 4 {
		return 0, false
	}
	for _, c := range values[0] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n < 1 || n > maxLimit {
		return 0, false
	}
	return n, true
}

// ReadObject enforces JSON media type, a body size bound, and the strict object
// decoder. On failure it writes the problem response and returns false.
func ReadObject(w http.ResponseWriter, r *http.Request, fields map[string]Field, bodyLimit int64) (map[string]any, bool) {
	return readObject(w, r, fields, bodyLimit, false)
}

func ReadObjectAllowEmpty(w http.ResponseWriter, r *http.Request, fields map[string]Field, bodyLimit int64) (map[string]any, bool) {
	return readObject(w, r, fields, bodyLimit, true)
}

func readObject(w http.ResponseWriter, r *http.Request, fields map[string]Field, bodyLimit int64, allowEmpty bool) (map[string]any, bool) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	charset, hasCharset := params["charset"]
	if err != nil || !strings.EqualFold(media, "application/json") || len(params) > 1 || (len(params) == 1 && (!hasCharset || !strings.EqualFold(charset, "utf-8"))) {
		ProblemResponse(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return nil, false
	}
	if r.ContentLength > bodyLimit {
		ProblemResponse(w, http.StatusRequestEntityTooLarge, "content_too_large")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
	if err != nil {
		ProblemResponse(w, http.StatusRequestEntityTooLarge, "content_too_large")
		return nil, false
	}
	values, err := decodeObject(body, fields, allowEmpty)
	if err != nil {
		ProblemResponse(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	return values, true
}

// DecodeObject decodes a JSON object whose keys come from fields, without
// duplicates, unknown keys, disallowed nulls, or wrongly typed values. The
// object must be non-empty and contain every required key.
func DecodeObject(body []byte, fields map[string]Field) (map[string]any, error) {
	return decodeObject(body, fields, false)
}
func decodeObject(body []byte, fields map[string]Field, allowEmpty bool) (map[string]any, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	values := make(map[string]any, len(fields))
	for dec.More() {
		keyToken, err := dec.Token()
		key, ok := keyToken.(string)
		field, allowed := fields[key]
		if err != nil || !ok || !allowed {
			return nil, fmt.Errorf("unexpected key")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid value")
		}
		if string(raw) == "null" {
			if field.Kind != KindNullableString && field.Kind != KindNullableObject {
				return nil, fmt.Errorf("invalid value")
			}
			values[key] = nil
			continue
		}
		switch field.Kind {
		case KindString, KindNullableString:
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("expected string")
			}
			values[key] = v
		case KindBool:
			var v bool
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("expected boolean")
			}
			values[key] = v
		case KindInteger:
			text := string(raw)
			if text == "" || strings.ContainsAny(text, ".eE") {
				return nil, fmt.Errorf("expected integer literal")
			}
			v, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
					if strings.HasPrefix(text, "-") {
						v = -1 << 63
					} else {
						v = 1<<63 - 1
					}
				} else {
					return nil, fmt.Errorf("expected integer")
				}
			}
			values[key] = v
		case KindNullableObject:
			if field.Fields == nil {
				return nil, fmt.Errorf("missing nested schema")
			}
			v, err := decodeObject(raw, field.Fields, false)
			if err != nil {
				return nil, err
			}
			values[key] = v
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing data")
	}
	if len(values) == 0 && !allowEmpty {
		return nil, fmt.Errorf("empty object")
	}
	for key, field := range fields {
		if _, present := values[key]; field.Required && !present {
			return nil, fmt.Errorf("missing key %s", key)
		}
	}
	return values, nil
}

// WriteJSON writes a no-store JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblemBody(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ProblemResponse writes an RFC 9457 problem with a stable machine-readable code.
func ProblemResponse(w http.ResponseWriter, status int, code string) {
	writeProblemBody(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code})
}

// QueryProblemResponse writes the 400 invalid_query problem naming the parameter.
func QueryProblemResponse(w http.ResponseWriter, parameter string) {
	writeProblemBody(w, http.StatusBadRequest, map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusBadRequest), "status": http.StatusBadRequest, "code": "invalid_query", "parameter": parameter})
}

// ValidationProblemResponse writes the 422 problem naming the field and code.
func ValidationProblemResponse(w http.ResponseWriter, field, code string) {
	writeProblemBody(w, http.StatusUnprocessableEntity, map[string]any{"type": "about:blank", "title": "Validation Failed", "status": 422, "code": code, "field": field})
}
