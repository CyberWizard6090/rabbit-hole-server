package editor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"
)

// State is what the storage holds for one document.
type State struct {
	Snapshot []byte
	Updates  [][]byte
	LastSeq  int64
}

// Loader reads the stored state of a document.
type Loader func(ctx context.Context) (State, error)

// Checker keeps a copy of recently edited documents in memory and uses it to check every update before it is
// stored: an update must decode, and the document must stay openable after it (format, root block, block count).
//
// The copy is only a cache of what the storage holds. It is thrown away whenever it could differ from the storage
// (a refused update was already applied to it, a write failed, somebody else wrote, a snapshot replaced the
// history) and is rebuilt from the storage on the next update.
type Checker struct {
	schema *Schema
	max    int

	mu   sync.Mutex
	docs map[uint]*cached
}

type cached struct {
	mu   sync.Mutex
	used time.Time // guarded by Checker.mu

	doc *crdt.Doc
	seq int64
	// enforce is false for a document that was already unopenable when it was loaded (stored before the checks
	// existed): refusing every update would not repair it, so only decoding is checked.
	enforce bool
}

// DefaultCachedDocuments is how many documents a Checker keeps in memory.
const DefaultCachedDocuments = 64

func NewChecker(schema *Schema, maxDocuments int) *Checker {
	if maxDocuments < 1 {
		maxDocuments = DefaultCachedDocuments
	}
	return &Checker{schema: schema, max: maxDocuments, docs: make(map[uint]*cached)}
}

func (c *Checker) entry(id uint) *cached {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.docs[id]
	if !ok {
		item = &cached{}
		c.docs[id] = item
	}
	item.used = time.Now()

	// Over the limit: forget the document that was edited longest ago. Its mutex is not taken, an update in
	// flight keeps working on its own copy and the next one loads the document again.
	for len(c.docs) > c.max {
		var oldest uint
		var when time.Time
		for key, other := range c.docs {
			if key != id && (when.IsZero() || other.used.Before(when)) {
				oldest, when = key, other.used
			}
		}
		delete(c.docs, oldest)
	}
	return item
}

// Forget drops the copy of a document (it was deleted, or its history was replaced).
func (c *Checker) Forget(id uint) {
	c.mu.Lock()
	item := c.docs[id]
	delete(c.docs, id)
	c.mu.Unlock()

	// An update that is being checked right now holds the entry: mark the copy as stale for it too.
	if item != nil {
		item.mu.Lock()
		item.doc = nil
		item.mu.Unlock()
	}
}

// Len is the number of documents in memory.
func (c *Checker) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.docs)
}

func apply(doc *crdt.Doc, update []byte) (ok bool) {
	// The decoder works on bytes from the network; a bug in it must not take the process down.
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return crdt.ApplyUpdateV1(doc, update, nil) == nil
}

func (c *Checker) load(ctx context.Context, id uint, item *cached, load Loader) error {
	state, err := load(ctx)
	if err != nil {
		return err
	}
	doc := crdt.New()
	skipped := 0
	if len(state.Snapshot) > 0 && !apply(doc, state.Snapshot) {
		// A partly applied snapshot is useless: start from an empty document.
		doc, skipped = crdt.New(), 1
	}
	for _, update := range state.Updates {
		if !apply(doc, update) {
			skipped++
		}
	}
	item.doc, item.seq = doc, state.LastSeq
	item.enforce = skipped == 0 && checkOpenable(c.schema, doc) == nil
	if !item.enforce {
		slog.Warn("document cannot be checked: its stored state is already broken", "document_id", id, "undecodable", skipped)
	}
	return nil
}

// Append checks update against the document and, when it is acceptable, calls store, which must write it and
// return its number. Updates of one document are checked and stored one at a time.
//
// It returns ErrInvalidDocument (wrapped) for an update that does not decode or would make the document
// impossible to open; store is not called then. Other errors come from load or store.
func (c *Checker) Append(ctx context.Context, id uint, update []byte, load Loader, store func() (int64, error)) (int64, error) {
	// Bytes that do not decode at all are refused before the copy is touched: decoding into an empty document
	// costs as much as the update is long, while a refused update that reached the copy forces a reload.
	if !apply(crdt.New(), update) {
		return 0, invalid("update cannot be decoded")
	}

	item := c.entry(id)
	item.mu.Lock()
	defer item.mu.Unlock()

	if item.doc == nil {
		if err := c.load(ctx, id, item, load); err != nil {
			return 0, err
		}
	}

	if !apply(item.doc, update) {
		item.doc = nil // a part of the update may have been applied
		return 0, invalid("update cannot be decoded")
	}
	if item.enforce {
		if err := checkOpenable(c.schema, item.doc); err != nil {
			item.doc = nil
			return 0, err
		}
	}

	seq, err := store()
	if err != nil {
		item.doc = nil // the copy has an update the storage does not
		return 0, err
	}
	if seq != item.seq+1 {
		item.doc = nil // somebody else wrote in between (another process, or a snapshot)
		return seq, nil
	}
	item.seq = seq
	return seq, nil
}

// checkOpenable is the part of Inspect a client depends on to open the document at all. Unlike Inspect it does
// not walk every block, so it is cheap enough to run on each update.
func checkOpenable(schema *Schema, doc *crdt.Doc) error {
	meta := doc.GetMap("meta")
	if format, ok := meta.Get("format"); !ok || !isNumber(format, FormatVersion) {
		return invalid("meta.format is not %d", FormatVersion)
	}
	rootID, ok := mapGet[string](meta, "rootId")
	if !ok || rootID == "" {
		return invalid("meta.rootId is missing")
	}
	blocks := doc.GetMap("blocks")
	root, ok := readBlock(blocks, rootID)
	if !ok {
		return invalid("root block %q is missing or malformed", rootID)
	}
	if rootSchema, known := schema.Blocks[root.flavour]; !known || rootSchema.Role != rootSchemaRole {
		return invalid("root block has flavour %q, which cannot be a root", root.flavour)
	}
	if count := len(blocks.Keys()); count > MaxBlocks {
		return invalid("too many blocks (%d, the limit is %d)", count, MaxBlocks)
	}
	return nil
}
