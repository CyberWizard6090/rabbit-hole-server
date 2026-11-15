package editor

import (
	"errors"

	"github.com/reearth/ygo/crdt"
)

// ErrNotCompactable means the stored history must be left as it is.
var ErrNotCompactable = errors.New("history cannot be compacted")

// Compact builds one state from a snapshot and the updates after it: the document is assembled and encoded again,
// which also drops the content that was deleted. The result replaces the history up to the last update.
//
// Anything unusual refuses to compact instead of guessing: an update that does not decode, one that waits for data
// the history does not have, a document that would not open, or a result larger than maxBytes. Compacting is only
// an optimisation, so leaving the history alone is always safe.
func Compact(schema *Schema, state State, maxBytes int) (compacted []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			compacted, err = nil, ErrNotCompactable
		}
	}()

	doc := crdt.New()
	if len(state.Snapshot) > 0 && !apply(doc, state.Snapshot) {
		return nil, ErrNotCompactable
	}
	for _, update := range state.Updates {
		if !apply(doc, update) {
			return nil, ErrNotCompactable
		}
	}
	if pending := doc.PendingStats(); pending.Items != 0 || pending.DeleteRanges != 0 {
		return nil, ErrNotCompactable
	}
	if checkOpenable(schema, doc) != nil {
		return nil, ErrNotCompactable
	}
	compacted = crdt.EncodeStateAsUpdateV1(doc, nil)
	if len(compacted) == 0 || len(compacted) > maxBytes {
		return nil, ErrNotCompactable
	}
	// The result must give the same document when it is applied to an empty one.
	if _, err := Inspect(schema, compacted); err != nil {
		return nil, ErrNotCompactable
	}
	return compacted, nil
}
