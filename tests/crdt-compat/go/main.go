// Command crdt-compat applies updates produced by the yjs npm package with the Go port and writes what it
// sees and what it encodes back to out/, where js/verify.mjs compares them with yjs.
//
// ORDER=shuffle applies the incremental updates in a random order, anything else in the order they were made.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"

	"github.com/reearth/ygo/crdt"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func write(name string, data []byte) {
	must(os.WriteFile("out/"+name, data, 0o600))
}

func writeJSON(name string, value any) {
	raw, err := json.Marshal(value)
	must(err)
	write(name, raw)
}

func child[T any](parent *crdt.YMap, key string) T {
	value, ok := parent.Get(key)
	if !ok {
		panic(fmt.Sprintf("key %q is missing", key))
	}
	return value.(T)
}

// project mirrors project() in js/common.mjs. YMap.ForEach skips nested shared types, so the walk goes
// through Keys and Get.
func project(doc *crdt.Doc) map[string]any {
	out := map[string]any{}
	blocks := doc.GetMap("blocks")
	for _, id := range blocks.Keys() {
		block := child[*crdt.YMap](blocks, id)
		flavour, _ := block.Get("flavour")
		place, _ := block.Get("place")

		var props any
		raw, err := child[*crdt.YMap](block, "props").ToJSON()
		must(err)
		must(json.Unmarshal(raw, &props))

		delta := []any{}
		for _, op := range child[*crdt.YText](block, "text").ToDelta() {
			entry := map[string]any{"insert": op.Insert}
			if len(op.Attributes) > 0 {
				entry["attributes"] = op.Attributes
			}
			delta = append(delta, entry)
		}
		out[id] = map[string]any{"flavour": flavour, "place": place, "props": props, "delta": delta}
	}
	return out
}

func main() {
	raw, err := os.ReadFile("out/updates.json")
	must(err)
	var encoded []string
	must(json.Unmarshal(raw, &encoded))
	updates := make([][]byte, len(encoded))
	for i, item := range encoded {
		updates[i], err = base64.StdEncoding.DecodeString(item)
		must(err)
	}

	order := make([]int, len(updates))
	for i := range order {
		order[i] = i
	}
	if os.Getenv("ORDER") == "shuffle" {
		order = rand.New(rand.NewSource(7)).Perm(len(updates)) //nolint:gosec // test data, not a secret
	}
	writeJSON("go_order.json", order)

	// Incremental updates, as a relay would receive them.
	doc := crdt.New()
	for _, i := range order {
		must(crdt.ApplyUpdateV1(doc, updates[i], "remote"))
	}
	if pending := doc.PendingStats(); pending.Items != 0 || pending.DeleteRanges != 0 {
		panic(fmt.Sprintf("updates are still pending: %+v", pending))
	}
	writeJSON("go_incremental.json", project(doc))
	write("go_state.bin", crdt.EncodeStateAsUpdateV1(doc, nil))
	write("go_state_v2.bin", crdt.EncodeStateAsUpdateV2(doc, nil))
	write("go_sv.bin", crdt.EncodeStateVectorV1(doc))

	// Compaction without building a document.
	merged, err := crdt.MergeUpdatesV1(updates...)
	must(err)
	write("go_merged.bin", merged)

	// A full state encoded by yjs.
	state, err := os.ReadFile("out/js_state.bin")
	must(err)
	fromState := crdt.New()
	must(crdt.ApplyUpdateV1(fromState, state, nil))
	writeJSON("go_from_state.json", project(fromState))

	// Edits made on the server: text with attributes, a map key, a new block with nested types.
	blocks := doc.GetMap("blocks")
	ids := blocks.Keys()
	sort.Strings(ids)
	first := child[*crdt.YMap](blocks, ids[0])
	// Reads take the document lock that Transact already holds, so shared types are looked up beforehand.
	text, props := child[*crdt.YText](first, "text"), child[*crdt.YMap](first, "props")
	doc.Transact(func(txn *crdt.Transaction) {
		text.Insert(txn, 0, "GO🙂 ", crdt.Attributes{"bold": true})
		props.Set(txn, "serverStamp", "ok")
		blocks.Set(txn, "go-block", crdt.NewMapPrelim())
	}, "server")
	created := child[*crdt.YMap](blocks, "go-block")
	doc.Transact(func(txn *crdt.Transaction) {
		created.Set(txn, "flavour", "block:paragraph")
		created.Set(txn, "place", map[string]any{"p": "root", "k": "zz"})
		created.Set(txn, "props", crdt.NewMapPrelim())
		created.Set(txn, "text", crdt.NewTextPrelim())
	}, "server")
	writeJSON("go_after_edit.json", project(doc))
	write("go_state_after_edit.bin", crdt.EncodeStateAsUpdateV1(doc, nil))
}
