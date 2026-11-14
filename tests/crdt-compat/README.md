# Yjs ↔ ygo compatibility check

Checks that the Go port of Yjs ([`reearth/ygo`](https://pkg.go.dev/github.com/reearth/ygo)) reads and writes the
same data as the `yjs` npm package, on the document layout planned for notes
([`docs/collab-architecture.md`](../../docs/collab-architecture.md)).

It is a separate Go module and a separate npm package on purpose: the server's `go.mod` does not depend on `ygo`
yet, and nothing here runs in CI. When the engine is added to the server, this check moves into CI in the same
change and runs whenever `ygo` or `yjs` is updated.

## Run

Needs Go 1.24+ and Node 20+.

```bash
./run.sh        # 10 seeds
./run.sh 40     # more seeds
```

Each seed runs twice: 3000 edits applied in the order they were made, and 600 edits applied in a random order.
A line starts with `ok` or `FAIL`; the script exits with 1 if any check failed.

## What happens

1. `js/gen.mjs`: three `yjs` replicas make random edits (text with Cyrillic, emoji and the object character,
   formatting attributes, props, moves, type changes, block deletion) and sync now and then. It writes every
   incremental update, the merged state and the expected projection to `out/`.
2. `go/main.go`: applies the updates with `ygo`, then writes what it sees and what it encodes: its state as V1
   and V2, the state vector, all updates merged without a document, and the state after edits of its own.
3. `js/verify.mjs`: compares with `yjs`.

| Check | Meaning |
|---|---|
| `goReadsYjsState` | Go reads a full state encoded by `yjs` |
| `goIncrementalMatchesYjs` | Go and a `yjs` replica that got the same updates in the same order see the same document |
| `yjsReadsGoV1`, `yjsReadsGoV2` | `yjs` reads the state Go encodes |
| `yjsReadsGoMergedUpdates` | updates merged by Go without a document give the expected document |
| `stateVectorsEqual` | both sides report the same state vector |
| `yjsAppliesGoEdit` | edits made in Go (text, map key, new block with nested types) arrive in `yjs` |
| `yjsEditsOnGoState` | `yjs` keeps editing on top of a state produced by Go and still converges |

`orderDependentFormatting: true` is reported but is not a failure. `yjs` removes redundant formatting marks when
it applies remote changes, and which marks it removes depends on the delivery order. Until two replicas exchange
their full delete sets (sync step 2), one block may be formatted differently on one of them. Go behaves the same
as `yjs` here, which `goIncrementalMatchesYjs` verifies.

## Result

`yjs` 13.6.33, `ygo` 1.55.0, `./run.sh 40`: 80 runs, all checks passed, `orderDependentFormatting` in one run.
The state Go encodes as V1 was 27–74 % larger than the `yjs` one; as V2 it was between 8 % smaller and 21 %
larger than `yjs` V1.

## Not covered

Garbage collection, updates that stay pending, awareness, the sync handshake over a socket, large documents,
performance.
