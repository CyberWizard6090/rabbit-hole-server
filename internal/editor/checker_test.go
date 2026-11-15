package editor

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"

	"github.com/reearth/ygo/crdt"
)

// storage is an in-memory stand-in for the database of one document.
type storage struct {
	mu       sync.Mutex
	snapshot []byte
	updates  [][]byte
	loads    int
	failNext error
}

func (s *storage) load(context.Context) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return State{Snapshot: s.snapshot, Updates: append([][]byte(nil), s.updates...), LastSeq: int64(len(s.updates))}, nil
}

func (s *storage) store(update []byte) func() (int64, error) {
	return func() (int64, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.failNext; err != nil {
			s.failNext = nil
			return 0, err
		}
		s.updates = append(s.updates, update)
		return int64(len(s.updates)), nil
	}
}

func (s *storage) append(t *testing.T, checker *Checker, update []byte) error {
	t.Helper()
	_, err := checker.Append(context.Background(), 1, update, s.load, s.store(update))
	return err
}

// client is an editor that has the document and produces updates.
type client struct{ doc *crdt.Doc }

func newClient(t *testing.T, state []byte) *client {
	t.Helper()
	doc := crdt.New()
	if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
		t.Fatal(err)
	}
	return &client{doc: doc}
}

func (c *client) edit(change func(txn *crdt.Transaction)) []byte {
	before := c.doc.StateVector()
	c.doc.Transact(change, nil)
	return crdt.EncodeStateAsUpdateV1(c.doc, before)
}

func (c *client) text(id string) *crdt.YText {
	value, _ := c.doc.GetMap("blocks").Get(id)
	text, _ := value.(*crdt.YMap).Get("text")
	return text.(*crdt.YText)
}

func (c *client) typeInto(id, s string) []byte {
	text := c.text(id)
	return c.edit(func(txn *crdt.Transaction) { text.Insert(txn, 0, s, nil) })
}

func setup(t *testing.T) (*storage, *Checker, *client) {
	t.Helper()
	snapshot := state(t, valid)
	return &storage{snapshot: snapshot}, NewChecker(DefaultSchema(), 4), newClient(t, snapshot)
}

func TestCheckerAcceptsEdits(t *testing.T) {
	db, checker, author := setup(t)
	for i := 0; i < 50; i++ {
		if err := db.append(t, checker, author.typeInto("p1", "x")); err != nil {
			t.Fatalf("edit %d: %v", i, err)
		}
	}
	if db.loads != 1 || len(db.updates) != 50 {
		t.Fatalf("loads=%d stored=%d: the document must be loaded once", db.loads, len(db.updates))
	}
}

func TestCheckerRefusesGarbageAndRecovers(t *testing.T) {
	db, checker, author := setup(t)
	if err := db.append(t, checker, author.typeInto("p1", "a")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte("garbage"), {0}, {1, 2, 3, 4, 5, 6, 7, 8}, author.typeInto("p1", "lost")[:6]} {
		if err := db.append(t, checker, raw); !errors.Is(err, ErrInvalidDocument) {
			t.Fatalf("garbage %v: err = %v", raw, err)
		}
	}
	// The author goes on typing; the refused updates left nothing behind. (The truncated one was a real edit, so
	// this update depends on it and waits for it: it is stored, see TestCheckerAcceptsUpdatesThatWait.)
	if err := db.append(t, checker, author.typeInto("p1", "b")); err != nil {
		t.Fatal(err)
	}
	if len(db.updates) != 2 {
		t.Fatalf("stored %d updates, want 2", len(db.updates))
	}
}

func TestCheckerRefusesUpdatesThatMakeTheDocumentUnopenable(t *testing.T) {
	cases := map[string]func(c *client) []byte{
		"root deleted": func(c *client) []byte {
			blocks := c.doc.GetMap("blocks")
			return c.edit(func(txn *crdt.Transaction) { blocks.Delete(txn, "root") })
		},
		"root replaced by a string": func(c *client) []byte {
			blocks := c.doc.GetMap("blocks")
			return c.edit(func(txn *crdt.Transaction) { blocks.Set(txn, "root", "oops") })
		},
		"root turned into a paragraph": func(c *client) []byte {
			value, _ := c.doc.GetMap("blocks").Get("root")
			return c.edit(func(txn *crdt.Transaction) { value.(*crdt.YMap).Set(txn, "flavour", "block:paragraph") })
		},
		"format changed": func(c *client) []byte {
			meta := c.doc.GetMap("meta")
			return c.edit(func(txn *crdt.Transaction) { meta.Set(txn, "format", 99) })
		},
		"rootId removed": func(c *client) []byte {
			meta := c.doc.GetMap("meta")
			return c.edit(func(txn *crdt.Transaction) { meta.Delete(txn, "rootId") })
		},
		"rootId points nowhere": func(c *client) []byte {
			meta := c.doc.GetMap("meta")
			return c.edit(func(txn *crdt.Transaction) { meta.Set(txn, "rootId", "ghost") })
		},
	}
	for name, attack := range cases {
		t.Run(name, func(t *testing.T) {
			db, checker, author := setup(t)
			if err := db.append(t, checker, author.typeInto("p1", "before")); err != nil {
				t.Fatal(err)
			}
			attacker := newClient(t, db.snapshot)
			if err := db.append(t, checker, attack(attacker)); !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("err = %v", err)
			}
			if len(db.updates) != 1 {
				t.Fatal("a refused update must not be stored")
			}
			if err := db.append(t, checker, author.typeInto("p1", "after")); err != nil {
				t.Fatalf("the document must stay editable: %v", err)
			}
			// What is stored opens as a valid document.
			merged := crdt.New()
			for _, raw := range append([][]byte{db.snapshot}, db.updates...) {
				if err := crdt.ApplyUpdateV1(merged, raw, nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Inspect(DefaultSchema(), crdt.EncodeStateAsUpdateV1(merged, nil)); err != nil {
				t.Fatalf("stored document: %v", err)
			}
		})
	}
}

func TestCheckerDoesNotRefuseSchemaViolations(t *testing.T) {
	db, checker, author := setup(t)
	blocks := author.doc.GetMap("blocks")
	// A list inside a paragraph: clients repair it when they project the document.
	update := author.edit(func(txn *crdt.Transaction) { blocks.Set(txn, "bad", crdt.NewMapPrelim()) })
	if err := db.append(t, checker, update); err != nil {
		t.Fatal(err)
	}
	value, _ := blocks.Get("bad")
	update = author.edit(func(txn *crdt.Transaction) {
		block := value.(*crdt.YMap)
		block.Set(txn, "flavour", "block:list")
		block.Set(txn, "place", map[string]any{"p": "p1", "k": "a"})
		block.Set(txn, "props", crdt.NewMapPrelim())
		block.Set(txn, "text", crdt.NewTextPrelim())
	})
	if err := db.append(t, checker, update); err != nil {
		t.Fatal(err)
	}
}

func TestCheckerAcceptsUpdatesThatWait(t *testing.T) {
	// The second edit arrives before the first (the first was refused by the rate limit and is sent again later).
	db, checker, author := setup(t)
	first, second := author.typeInto("p1", "1"), author.typeInto("p1", "2")
	if err := db.append(t, checker, second); err != nil {
		t.Fatalf("an update that waits for another one must be stored: %v", err)
	}
	if err := db.append(t, checker, first); err != nil {
		t.Fatal(err)
	}
}

func TestCheckerReloadsAfterAFailedWriteAndForget(t *testing.T) {
	db, checker, author := setup(t)
	if err := db.append(t, checker, author.typeInto("p1", "a")); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("database is down")
	db.failNext = boom
	if err := db.append(t, checker, author.typeInto("p1", "b")); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if err := db.append(t, checker, author.typeInto("p1", "c")); err != nil {
		t.Fatal(err)
	}
	if db.loads != 2 {
		t.Fatalf("loads = %d: the copy must be rebuilt after a failed write", db.loads)
	}
	checker.Forget(1)
	if checker.Len() != 0 {
		t.Fatal("Forget must drop the copy")
	}
	if err := db.append(t, checker, author.typeInto("p1", "d")); err != nil || db.loads != 3 {
		t.Fatalf("err=%v loads=%d", err, db.loads)
	}
}

func TestCheckerReloadsWhenSomebodyElseWrote(t *testing.T) {
	db, checker, author := setup(t)
	if err := db.append(t, checker, author.typeInto("p1", "a")); err != nil {
		t.Fatal(err)
	}
	// Another process stored an update the copy has not seen.
	other := newClient(t, db.snapshot)
	db.updates = append(db.updates, other.typeInto("p1", "z"))
	if err := db.append(t, checker, author.typeInto("p1", "b")); err != nil {
		t.Fatal(err)
	}
	if err := db.append(t, checker, author.typeInto("p1", "c")); err != nil || db.loads != 2 {
		t.Fatalf("err=%v loads=%d: a gap in the numbers must rebuild the copy", err, db.loads)
	}
}

func TestCheckerDoesNotEnforceOnADocumentThatWasAlreadyBroken(t *testing.T) {
	// Stored before the checks existed: garbage in the history and no root block.
	broken := state(t, func(doc *crdt.Doc) { meta(doc, "root") })
	db := &storage{snapshot: broken, updates: [][]byte{[]byte("old garbage")}}
	checker := NewChecker(DefaultSchema(), 4)
	author := newClient(t, broken)
	scratch := author.doc.GetText("scratch")
	update := author.edit(func(txn *crdt.Transaction) { scratch.Insert(txn, 0, "x", nil) })
	if err := db.append(t, checker, update); err != nil {
		t.Fatalf("a valid update of a broken document is stored: %v", err)
	}
	if err := db.append(t, checker, []byte("garbage")); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("garbage is still refused: %v", err)
	}
}

func TestCheckerKeepsAFewDocuments(t *testing.T) {
	snapshot := state(t, valid)
	checker := NewChecker(DefaultSchema(), 3)
	for id := uint(1); id <= 10; id++ {
		db := &storage{snapshot: snapshot}
		author := newClient(t, snapshot)
		update := author.typeInto("p1", "x")
		if _, err := checker.Append(context.Background(), id, update, db.load, db.store(update)); err != nil {
			t.Fatal(err)
		}
		if checker.Len() > 3 {
			t.Fatalf("%d documents in memory, the limit is 3", checker.Len())
		}
	}
}

func TestCheckerConcurrentWriters(t *testing.T) {
	db, checker, _ := setup(t)
	const writers, edits = 8, 40
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			author := newClient(t, db.snapshot)
			for i := 0; i < edits; i++ {
				update := author.typeInto("p1", "x")
				if _, err := checker.Append(context.Background(), 1, update, db.load, db.store(update)); err != nil {
					t.Errorf("writer %d: %v", w, err)
					return
				}
				if i%10 == 0 {
					checker.Forget(1)
				}
			}
		}(w)
	}
	wg.Wait()
	if len(db.updates) != writers*edits {
		t.Fatalf("stored %d, want %d", len(db.updates), writers*edits)
	}
}

func TestCheckerSurvivesMutatedUpdates(t *testing.T) {
	db, checker, author := setup(t)
	rnd := rand.New(rand.NewSource(7)) //nolint:gosec // test data
	accepted := 0
	for i := 0; i < 2000; i++ {
		good := author.typeInto("p1", "ab")
		bad := append([]byte(nil), good...)
		bad[rnd.Intn(len(bad))] ^= byte(1 + rnd.Intn(255))
		if db.append(t, checker, bad) == nil {
			accepted++
		}
		if err := db.append(t, checker, good); err != nil {
			t.Fatalf("step %d: a good update was refused after a mutated one: %v", i, err)
		}
	}
	// Whatever was accepted, the stored history still opens as a valid document.
	merged := crdt.New()
	for _, raw := range append([][]byte{db.snapshot}, db.updates...) {
		if !apply(merged, raw) {
			t.Fatal("a stored update does not decode")
		}
	}
	if err := checkOpenable(DefaultSchema(), merged); err != nil {
		t.Fatalf("after %d accepted mutations: %v", accepted, err)
	}
	t.Logf("mutated updates accepted: %d of 2000", accepted)
}
