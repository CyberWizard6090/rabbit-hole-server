// Package editor lets the server look inside a document without taking over its merging.
//
// Documents are Yjs states (format inkbrick-yjs v1, see Inkbrick docs/collab.md). Yjs merges edits, the server only
// stores them, so before this package a snapshot of random bytes was accepted and then nobody could open the
// document. Inspect applies a state to an empty document and checks the structure: what the client needs to
// open it (fatal problems) and what its schema forbids (issues the client heals itself).
//
// Schema violations are not rejected: two clients can legitimately create one by concurrent edits, and every
// client repairs it the same way when it projects the document (rule 4 in @inkbrick/collab).
package editor

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/reearth/ygo/crdt"
)

// FormatVersion is the value of meta.format the server understands.
const FormatVersion = 1

// Limits keep one document from using the server's memory for nothing.
const (
	MaxBlocks      = 50_000
	MaxIssues      = 50
	maxIDRunes     = 128
	rootSchemaRole = "root"
)

// ErrInvalidDocument is returned for a state that is not an inkbrick-yjs document.
var ErrInvalidDocument = errors.New("invalid document")

//go:embed schema.json
var schemaJSON []byte

type blockSchema struct {
	Role     string   `json:"role"`
	Inline   bool     `json:"inline"`
	Children []string `json:"children"`

	canContain map[string]struct{}
}

// Schema is the nesting rules shared with the editor (Inkbrick packages/collab/schema/blocks.json).
type Schema struct {
	Version int                     `json:"version"`
	Blocks  map[string]*blockSchema `json:"blocks"`
}

// LoadSchema parses a schema file.
func LoadSchema(raw []byte) (*Schema, error) {
	var schema Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if schema.Version != 1 || len(schema.Blocks) == 0 {
		return nil, errors.New("schema: unsupported version or no blocks")
	}
	for _, block := range schema.Blocks {
		block.canContain = make(map[string]struct{}, len(block.Children))
		for _, child := range block.Children {
			block.canContain[child] = struct{}{}
		}
	}
	return &schema, nil
}

// DefaultSchema is the schema embedded in the binary.
func DefaultSchema() *Schema {
	schema, err := LoadSchema(schemaJSON)
	if err != nil {
		panic(err) // the embedded file is checked by tests
	}
	return schema
}

// Report is what Inspect found in a valid document.
type Report struct {
	RootID string
	Blocks int
	// Issues lists schema violations and unreachable blocks, at most MaxIssues. The document stays valid.
	Issues []string
	// IssueCount is the real number of issues, Issues is cut at MaxIssues.
	IssueCount int
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDocument, fmt.Sprintf(format, args...))
}

type entry struct {
	flavour string
	parent  string // "" for no parent
	hasPar  bool
}

// Inspect applies state (a Yjs V1 update with the whole document) to an empty document and checks it.
func Inspect(schema *Schema, state []byte) (report *Report, err error) {
	if len(state) == 0 {
		return nil, invalid("empty state")
	}
	// The decoder works on bytes from the network; a bug in it must not take the process down.
	defer func() {
		if recovered := recover(); recovered != nil {
			report, err = nil, invalid("state cannot be decoded")
		}
	}()

	doc := crdt.New()
	if applyErr := crdt.ApplyUpdateV1(doc, state, nil); applyErr != nil {
		return nil, invalid("state cannot be decoded")
	}
	if pending := doc.PendingStats(); pending.Items != 0 || pending.DeleteRanges != 0 {
		return nil, invalid("state refers to data it does not contain")
	}
	return inspectDoc(schema, doc)
}

func inspectDoc(schema *Schema, doc *crdt.Doc) (*Report, error) {
	meta := doc.GetMap("meta")
	if format, ok := meta.Get("format"); !ok || !isNumber(format, FormatVersion) {
		return nil, invalid("meta.format is not %d", FormatVersion)
	}
	rootValue, _ := meta.Get("rootId")
	rootID, ok := rootValue.(string)
	if !ok || rootID == "" {
		return nil, invalid("meta.rootId is missing")
	}

	blocks := doc.GetMap("blocks")
	ids := blocks.Keys()
	if len(ids) > MaxBlocks {
		return nil, invalid("too many blocks (%d, the limit is %d)", len(ids), MaxBlocks)
	}
	sort.Strings(ids)

	report := &Report{RootID: rootID}
	issue := func(format string, args ...any) {
		report.IssueCount++
		if len(report.Issues) < MaxIssues {
			report.Issues = append(report.Issues, fmt.Sprintf(format, args...))
		}
	}

	entries := make(map[string]entry, len(ids))
	for _, id := range ids {
		if id == "" || len([]rune(id)) > maxIDRunes {
			return nil, invalid("block id has a wrong length")
		}
		block, ok := readBlock(blocks, id)
		if !ok {
			issue("block %q is malformed and will be skipped by clients", id)
			continue
		}
		entries[id] = block
	}

	root, ok := entries[rootID]
	if !ok {
		return nil, invalid("root block %q is missing or malformed", rootID)
	}
	if rootSchema, known := schema.Blocks[root.flavour]; !known || rootSchema.Role != rootSchemaRole {
		return nil, invalid("root block has flavour %q, which cannot be a root", root.flavour)
	}
	report.Blocks = len(entries)

	for _, id := range ids {
		block, ok := entries[id]
		if !ok || id == rootID {
			continue
		}
		known, flavourKnown := schema.Blocks[block.flavour]
		if !flavourKnown {
			issue("block %q has unknown flavour %q", id, block.flavour)
			continue
		}
		if known.Role == rootSchemaRole {
			issue("block %q is a %s but is not the root", id, block.flavour)
		}
		parent, hasParent := entries[block.parent]
		if !block.hasPar || !hasParent {
			issue("block %q has no parent in the document", id)
			continue
		}
		parentSchema, parentKnown := schema.Blocks[parent.flavour]
		if !parentKnown {
			continue
		}
		if _, allowed := parentSchema.canContain[block.flavour]; !allowed {
			issue("%s cannot be placed inside %s (block %q)", block.flavour, parent.flavour, id)
		}
	}
	return report, nil
}

func isNumber(value any, want int) bool {
	switch v := value.(type) {
	case int:
		return v == want
	case int64:
		return v == int64(want)
	case float64:
		return v == float64(want)
	}
	return false
}

// readBlock reads the fields a client needs to show a block. It mirrors rule 1 of the projection.
func readBlock(blocks *crdt.YMap, id string) (entry, bool) {
	value, ok := blocks.Get(id)
	if !ok {
		return entry{}, false
	}
	block, ok := value.(*crdt.YMap)
	if !ok {
		return entry{}, false
	}
	flavour, ok := mapGet[string](block, "flavour")
	if !ok {
		return entry{}, false
	}
	if _, ok := mapGet[*crdt.YMap](block, "props"); !ok {
		return entry{}, false
	}
	if _, ok := mapGet[*crdt.YText](block, "text"); !ok {
		return entry{}, false
	}
	place, ok := mapGet[map[string]any](block, "place")
	if !ok {
		return entry{}, false
	}
	if _, ok := place["k"].(string); !ok {
		return entry{}, false
	}
	switch parent := place["p"].(type) {
	case nil:
		return entry{flavour: flavour}, true
	case string:
		return entry{flavour: flavour, parent: parent, hasPar: true}, true
	}
	return entry{}, false
}

func mapGet[T any](m *crdt.YMap, key string) (T, bool) {
	var zero T
	value, ok := m.Get(key)
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}
