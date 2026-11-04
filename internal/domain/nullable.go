package domain

import "encoding/json"

// Nullable distinguishes a JSON field that was omitted (Set == false) from one
// sent as null (Set == true, Value == nil), so PATCH can clear a value.
type Nullable[T any] struct {
	Set   bool
	Value *T
}

func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
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
