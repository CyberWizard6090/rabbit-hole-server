package editor

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
)

// bigDoc builds a document with n paragraphs of about 200 characters each.
func bigDoc(n int) *crdt.Doc {
	doc := crdt.New()
	valid(doc)
	blocks := doc.GetMap("blocks")
	body := strings.Repeat("Съешь ещё этих мягких булок. ", 7)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("b%06d", i)
		addBlock(doc, id, "block:paragraph", "note", fmt.Sprintf("k%06d", i))
		value, _ := blocks.Get(id)
		text, _ := value.(*crdt.YMap).Get("text")
		doc.Transact(func(txn *crdt.Transaction) { text.(*crdt.YText).Insert(txn, 0, body, nil) }, nil)
	}
	return doc
}

// typing returns count updates, each one character typed by a client that has the document.
func typing(state []byte, count int) [][]byte {
	client := crdt.New()
	if err := crdt.ApplyUpdateV1(client, state, nil); err != nil {
		panic(err)
	}
	value, _ := client.GetMap("blocks").Get("p1")
	text, _ := value.(*crdt.YMap).Get("text")
	updates := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		before := client.StateVector()
		client.Transact(func(txn *crdt.Transaction) { text.(*crdt.YText).Insert(txn, 0, "x", nil) }, nil)
		updates = append(updates, crdt.EncodeStateAsUpdateV1(client, before))
	}
	return updates
}

func BenchmarkCheckerAppend(b *testing.B) {
	for _, size := range []int{100, 2000, 20000} {
		b.Run(fmt.Sprintf("blocks=%d", size), func(b *testing.B) {
			state := crdt.EncodeStateAsUpdateV1(bigDoc(size), nil)
			updates := typing(state, b.N)
			checker := NewChecker(DefaultSchema(), 4)
			load := func(context.Context) (State, error) { return State{Snapshot: state}, nil }
			seq := int64(0)
			store := func() (int64, error) { seq++; return seq, nil }
			if _, err := checker.Append(context.Background(), 1, typing(state, 1)[0], load, store); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(state))/1024, "stateKiB")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := checker.Append(context.Background(), 1, updates[i], load, store); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCheckerLoad(b *testing.B) {
	for _, size := range []int{100, 2000, 20000} {
		b.Run(fmt.Sprintf("blocks=%d", size), func(b *testing.B) {
			state := crdt.EncodeStateAsUpdateV1(bigDoc(size), nil)
			for i := 0; i < b.N; i++ {
				doc := crdt.New()
				if !apply(doc, state) {
					b.Fatal("apply")
				}
			}
		})
	}
}

func TestMemoryPerDocument(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	for _, size := range []int{100, 2000, 20000} {
		state := crdt.EncodeStateAsUpdateV1(bigDoc(size), nil)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		doc := crdt.New()
		apply(doc, state)
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("blocks=%d state=%d KiB heap=%d KiB", size, len(state)/1024, (int64(after.HeapAlloc)-int64(before.HeapAlloc))/1024)
		runtime.KeepAlive(doc)
	}
}
