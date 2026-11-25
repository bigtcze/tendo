package mcptool

import (
	"bytes"
	"encoding/json"
)

// Nullable is a tool argument that distinguishes three states: absent (Set
// false), explicit null (Set true, Value nil) and a value (Set true, Value
// non-nil). A plain pointer cannot tell absent from null.
type Nullable[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the argument was present. A JSON null leaves Value
// nil; any other value is decoded into Value.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		n.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}
