package mcptool

import (
	"fmt"

	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/google/jsonschema-go/jsonschema"
)

// RecurrenceArg is the repeat rule argument. All three fields are required.
type RecurrenceArg struct {
	IntervalValue int64  `json:"intervalValue" jsonschema:"how many units between repeats, 1 to 999"`
	IntervalUnit  string `json:"intervalUnit" jsonschema:"day, week, month or year"`
	Mode          string `json:"mode" jsonschema:"fixed or after_completion"`
}

// constraint tightens one property schema of a tool input.
type constraint = func(*jsonschema.Schema)

// datePattern is the wire form of a calendar date. The service still refuses an
// impossible date such as 2026-02-30.
const datePattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`

// setTypes replaces the inferred schema of a property with the given JSON types,
// keeping its description. One type is written as "type", several as "types".
func setTypes(s *jsonschema.Schema, types ...string) {
	desc := s.Description
	*s = jsonschema.Schema{Description: desc}
	if len(types) == 1 {
		s.Type = types[0]
		return
	}
	s.Types = types
}

func apply(s *jsonschema.Schema, constraints []constraint) {
	for _, c := range constraints {
		c(s)
	}
}

// plainString constrains a property to a string that is never null. An
// omitted property is absent; an explicit null is refused by the schema.
func plainString(constraints ...constraint) constraint {
	return func(s *jsonschema.Schema) {
		setTypes(s, "string")
		apply(s, constraints)
	}
}

// nullableString constrains a property to a string or an explicit null. Only
// the nullable fields use it: their null is a request to clear the value.
func nullableString(constraints ...constraint) constraint {
	return func(s *jsonschema.Schema) {
		setTypes(s, "string", "null")
		apply(s, constraints)
	}
}

// plainBool constrains a property to a boolean that is never null.
func plainBool(s *jsonschema.Schema) { setTypes(s, "boolean") }

// minLength constrains a string to at least n characters.
func minLength(n int) constraint {
	return func(s *jsonschema.Schema) { s.MinLength = &n }
}

// dateFormat pins a string property to the YYYY-MM-DD wire form.
func dateFormat(s *jsonschema.Schema) { s.Pattern = datePattern }

// minimumOne constrains an integer to at least 1 (versions and counts).
func minimumOne(s *jsonschema.Schema) {
	lo := 1.0
	s.Minimum = &lo
}

// recurrenceInterval is the service's accepted interval range (validateRecurrence).
const recurrenceMaxInterval = 999

// recurrenceArg constrains a repeat rule property to an object or an explicit
// null. The object requires all three fields, bounds each to the service's
// values and refuses unknown properties. A null clears the rule.
func recurrenceArg(s *jsonschema.Schema) {
	object, err := jsonschema.For[RecurrenceArg](nil)
	if err != nil {
		panic(fmt.Sprintf("recurrence schema: %v", err))
	}
	identityhttp.MCPRange(1, recurrenceMaxInterval)(object.Properties["intervalValue"])
	identityhttp.MCPEnum(enumValues(schedule.UnitDay, schedule.UnitWeek, schedule.UnitMonth, schedule.UnitYear)...)(object.Properties["intervalUnit"])
	identityhttp.MCPEnum(enumValues(schedule.ModeFixed, schedule.ModeAfterCompletion)...)(object.Properties["mode"])
	desc := s.Description
	*s = *object
	s.Type = ""
	s.Types = []string{"object", "null"}
	s.Description = desc
}

func enumValues[T ~string](values ...T) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return out
}
