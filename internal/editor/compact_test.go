package editor

import (
	"errors"
	"testing"

	"github.com/reearth/ygo/crdt"
)

func TestCompactKeepsTheDocumentAndDropsDeletedContent(t *testing.T) {
	snapshot := state(t, valid)
	author := newClient(t, snapshot)
	var updates [][]byte
	for i := 0; i < 40; i++ {
		updates = append(updates, author.typeInto("p1", "abcdefghij"))
	}
	text := author.text("p1")
	updates = append(updates, author.edit(func(txn *crdt.Transaction) { text.Delete(txn, 0, 380) }))

	compacted, err := Compact(DefaultSchema(), State{Snapshot: snapshot, Updates: updates, LastSeq: 41}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	whole := crdt.New()
	for _, raw := range append([][]byte{snapshot}, updates...) {
		apply(whole, raw)
	}
	fromCompacted := crdt.New()
	if !apply(fromCompacted, compacted) {
		t.Fatal("compacted state does not decode")
	}
	for _, doc := range []*crdt.Doc{whole, fromCompacted} {
		value, _ := doc.GetMap("blocks").Get("p1")
		inner, _ := value.(*crdt.YMap).Get("text")
		if got := inner.(*crdt.YText).ToString(); got != "abcdefghijabcdefghij" {
			t.Fatalf("text = %q", got)
		}
	}
	var total int
	for _, raw := range updates {
		total += len(raw)
	}
	if len(compacted) >= len(snapshot)+total {
		t.Fatalf("compacted %d bytes, history %d bytes: nothing was saved", len(compacted), len(snapshot)+total)
	}
	// Edits made on top of the old history still apply to the compacted state.
	more := author.typeInto("p1", "!")
	if !apply(fromCompacted, more) {
		t.Fatal("an edit made before compaction cannot be applied to the compacted state")
	}
}

func TestCompactLeavesUnusualHistoryAlone(t *testing.T) {
	snapshot := state(t, valid)
	author := newClient(t, snapshot)
	first, second := author.typeInto("p1", "1"), author.typeInto("p1", "2")
	broken := state(t, func(doc *crdt.Doc) { meta(doc, "root") })

	cases := map[string]State{
		"undecodable update":   {Snapshot: snapshot, Updates: [][]byte{[]byte("garbage")}},
		"update that waits":    {Snapshot: snapshot, Updates: [][]byte{second}},
		"broken document":      {Snapshot: broken},
		"undecodable snapshot": {Snapshot: []byte("garbage")},
	}
	for name, input := range cases {
		if _, err := Compact(DefaultSchema(), input, 1<<20); !errors.Is(err, ErrNotCompactable) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Compact(DefaultSchema(), State{Snapshot: snapshot, Updates: [][]byte{first, second}}, 10); !errors.Is(err, ErrNotCompactable) {
		t.Errorf("too large: err = %v", err)
	}
}

func TestClaimCompaction(t *testing.T) {
	db, checker, author := setup(t)
	if checker.ClaimCompaction(1, 3) {
		t.Fatal("a document that is not in memory is not compacted")
	}
	for i := 0; i < 4; i++ {
		if err := db.append(t, checker, author.typeInto("p1", "x")); err != nil {
			t.Fatal(err)
		}
	}
	if checker.ClaimCompaction(1, 5) {
		t.Fatal("4 updates are not enough for 5")
	}
	if !checker.ClaimCompaction(1, 4) {
		t.Fatal("4 updates are enough for 4")
	}
	if checker.ClaimCompaction(1, 4) {
		t.Fatal("one compaction at a time")
	}
	checker.DoneCompaction(1, 4)
	if checker.ClaimCompaction(1, 1) {
		t.Fatal("after a compaction the count starts from the new snapshot")
	}
	if err := db.append(t, checker, author.typeInto("p1", "y")); err != nil {
		t.Fatal(err)
	}
	if !checker.ClaimCompaction(1, 1) {
		t.Fatal("one more update since the snapshot")
	}
}
