package httpapi

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// MCPUUIDExpr is the unanchored identifier expression, for building patterns.
const MCPUUIDExpr = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

// MCPUUIDPattern is the input pattern for an identifier argument. Matching it
// does not make the identifier canonical; the service still decides and an
// unknown or foreign identifier is not found.
const MCPUUIDPattern = `^` + MCPUUIDExpr + `$`

// MCPInputSchema infers the input schema of T (an object that refuses unknown
// properties) and lets the tool tighten its properties: enums, bounds, lengths
// and patterns. It panics when the type cannot be inferred or a constrained
// property does not exist, so a schema mistake fails the build check at start.
func MCPInputSchema[T any](constraints map[string]func(*jsonschema.Schema)) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("mcp input schema: %v", err))
	}
	for name, constrain := range constraints {
		property, ok := schema.Properties[name]
		if !ok {
			panic(fmt.Sprintf("mcp input schema: unknown property %q", name))
		}
		constrain(property)
	}
	return schema
}

// MCPUUID constrains a property to the identifier pattern.
func MCPUUID(s *jsonschema.Schema) { s.Pattern = MCPUUIDPattern }

// MCPEnum constrains a string property to the given values.
func MCPEnum(values ...string) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		s.Enum = make([]any, 0, len(values))
		for _, v := range values {
			s.Enum = append(s.Enum, v)
		}
	}
}

// MCPRange constrains an integer property to [low, high].
func MCPRange(low, high int) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		lo, hi := float64(low), float64(high)
		s.Minimum, s.Maximum = &lo, &hi
	}
}

// MCPMaxLength constrains a string property to at most n characters.
func MCPMaxLength(n int) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) { s.MaxLength = &n }
}

// MCPIdempotencyKeyPattern is the input pattern of an idempotencyKey argument:
// the HTTP Idempotency-Key rule, 1 to 128 bytes of visible ASCII.
const MCPIdempotencyKeyPattern = `^[!-~]{1,128}$`

// MCPIdempotencyKeyArg constrains an idempotencyKey property and describes it.
func MCPIdempotencyKeyArg(s *jsonschema.Schema) {
	s.Pattern = MCPIdempotencyKeyPattern
	s.Description = "a new unique value (for example a UUID) for each distinct action; reuse it only to retry the same action"
}
