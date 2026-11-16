package editor

import (
	"errors"
	"math/rand"
	"os"
	"testing"

	"github.com/reearth/ygo/crdt"

	"rabbit-hole-server/internal/collab"
)

func state(t *testing.T, build func(doc *crdt.Doc)) []byte {
	t.Helper()
	doc := crdt.New()
	build(doc)
	return crdt.EncodeStateAsUpdateV1(doc, nil)
}

func addBlock(doc *crdt.Doc, id, flavour string, parent any, key string) {
	blocks := doc.GetMap("blocks")
	doc.Transact(func(txn *crdt.Transaction) {
		blocks.Set(txn, id, crdt.NewMapPrelim())
	}, nil)
	value, _ := blocks.Get(id)
	block := value.(*crdt.YMap)
	doc.Transact(func(txn *crdt.Transaction) {
		block.Set(txn, "flavour", flavour)
		block.Set(txn, "place", map[string]any{"p": parent, "k": key})
		block.Set(txn, "props", crdt.NewMapPrelim())
		block.Set(txn, "text", crdt.NewTextPrelim())
	}, nil)
}

func meta(doc *crdt.Doc, root string) {
	m := doc.GetMap("meta")
	doc.Transact(func(txn *crdt.Transaction) {
		m.Set(txn, "format", 1)
		m.Set(txn, "rootId", root)
	}, nil)
}

func valid(doc *crdt.Doc) {
	meta(doc, "root")
	addBlock(doc, "root", "block:page", nil, "")
	addBlock(doc, "note", "block:note", "root", "a")
	addBlock(doc, "p1", "block:paragraph", "note", "a")
}

func TestInitialStateIsValid(t *testing.T) {
	report, err := Inspect(DefaultSchema(), collab.InitialState())
	if err != nil {
		t.Fatal(err)
	}
	if report.IssueCount != 0 || report.Blocks < 3 {
		t.Fatalf("report = %+v", report)
	}
}

func TestValidDocument(t *testing.T) {
	report, err := Inspect(DefaultSchema(), state(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if report.Blocks != 3 || report.RootID != "root" || report.IssueCount != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRejects(t *testing.T) {
	cases := map[string][]byte{
		"empty":     nil,
		"garbage":   []byte("this is not a yjs update"),
		"truncated": state(t, valid)[:20],
		"no meta": state(t, func(doc *crdt.Doc) {
			addBlock(doc, "root", "block:page", nil, "")
		}),
		"no root block": state(t, func(doc *crdt.Doc) {
			meta(doc, "root")
			addBlock(doc, "note", "block:note", "root", "a")
		}),
		"root is not a page": state(t, func(doc *crdt.Doc) {
			meta(doc, "root")
			addBlock(doc, "root", "block:paragraph", nil, "")
		}),
		"wrong format": state(t, func(doc *crdt.Doc) {
			m := doc.GetMap("meta")
			doc.Transact(func(txn *crdt.Transaction) {
				m.Set(txn, "format", 2)
				m.Set(txn, "rootId", "root")
			}, nil)
			addBlock(doc, "root", "block:page", nil, "")
		}),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect(DefaultSchema(), raw); !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSchemaViolationsAreIssuesNotErrors(t *testing.T) {
	raw := state(t, func(doc *crdt.Doc) {
		valid(doc)
		addBlock(doc, "bad", "block:list", "p1", "a")           // list in a paragraph
		addBlock(doc, "orphan", "block:paragraph", "gone", "a") // parent does not exist
		addBlock(doc, "alien", "block:hologram", "note", "b")   // unknown flavour
	})
	report, err := Inspect(DefaultSchema(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.IssueCount != 3 {
		t.Fatalf("issues = %v", report.Issues)
	}
}

func TestMalformedBlockIsAnIssue(t *testing.T) {
	raw := state(t, func(doc *crdt.Doc) {
		valid(doc)
		blocks := doc.GetMap("blocks")
		doc.Transact(func(txn *crdt.Transaction) { blocks.Set(txn, "weird", "just a string") }, nil)
	})
	report, err := Inspect(DefaultSchema(), raw)
	if err != nil || report.IssueCount != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRandomBytesNeverPanic(t *testing.T) {
	rnd := rand.New(rand.NewSource(1)) //nolint:gosec // test data
	good := state(t, valid)
	for i := 0; i < 3000; i++ {
		raw := make([]byte, rnd.Intn(200))
		rnd.Read(raw)
		_, _ = Inspect(DefaultSchema(), raw)

		mutated := append([]byte(nil), good...)
		mutated[rnd.Intn(len(mutated))] ^= byte(1 + rnd.Intn(255))
		_, _ = Inspect(DefaultSchema(), mutated)
	}
}

func TestEmbeddedSchemaMatchesFile(t *testing.T) {
	raw, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSchema(raw); err != nil {
		t.Fatal(err)
	}
	if DefaultSchema().Blocks["block:page"].Role != "root" {
		t.Fatal("page must be the root")
	}
}

func TestSchemaKnowsTablesColumnsAndToggles(t *testing.T) {
	raw := state(t, func(doc *crdt.Doc) {
		valid(doc)
		addBlock(doc, "t", "block:table", "note", "b")
		addBlock(doc, "r", "block:table-row", "t", "a")
		addBlock(doc, "c", "block:table-cell", "r", "a")
		addBlock(doc, "cols", "block:columns", "note", "c")
		addBlock(doc, "col", "block:column", "cols", "a")
		addBlock(doc, "cp", "block:paragraph", "col", "a")
		addBlock(doc, "tg", "block:toggle", "note", "d")
		addBlock(doc, "tp", "block:paragraph", "tg", "a")
	})
	report, err := Inspect(DefaultSchema(), raw)
	if err != nil || report.IssueCount != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	raw = state(t, func(doc *crdt.Doc) {
		valid(doc)
		addBlock(doc, "t", "block:table", "note", "b")
		addBlock(doc, "bad", "block:table-cell", "t", "a")    // a cell directly in a table
		addBlock(doc, "lone", "block:table-row", "note", "c") // a row outside a table
	})
	report, err = Inspect(DefaultSchema(), raw)
	if err != nil || report.IssueCount != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
