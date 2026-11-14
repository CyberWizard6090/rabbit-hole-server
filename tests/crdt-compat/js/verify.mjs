// Compares what the Go program wrote to out/ with yjs. Exits with 1 when a check fails.
import fs from 'node:fs';

import { Y, project, canonical, normalizeProjection } from './common.mjs';

const bytes = (name) => fs.readFileSync(`out/${name}`);
const json = (name) => JSON.parse(fs.readFileSync(`out/${name}`, 'utf8'));
const goProjection = (name) => canonical(normalizeProjection(json(name)));
const load = (name, apply = Y.applyUpdate) => {
  const doc = new Y.Doc();

  apply(doc, bytes(name));

  return doc;
};
const view = (doc) => canonical(project(doc));

const expected = canonical(json('expected.json'));
const updates = json('updates.json').map((item) => Buffer.from(item, 'base64'));
const order = json('go_order.json');

// A yjs replica that receives the same updates in the same order as the Go document did.
const sameOrder = new Y.Doc();

for (const index of order) Y.applyUpdate(sameOrder, updates[index]);

const reference = load('js_state.bin');
const stateVector = (raw) => canonical([...Y.decodeStateVector(raw)].sort((a, b) => a[0] - b[0]));

const checks = {
  // Go reads a full yjs state.
  goReadsYjsState: goProjection('go_from_state.json') === expected,
  // Go applies incremental updates exactly like yjs does for the same delivery order.
  goIncrementalMatchesYjs: goProjection('go_incremental.json') === view(sameOrder),
  // yjs reads what Go encodes (V1, V2) and gets what Go itself sees.
  yjsReadsGoV1: view(load('go_state.bin')) === goProjection('go_incremental.json'),
  yjsReadsGoV2: view(load('go_state_v2.bin', Y.applyUpdateV2)) === goProjection('go_incremental.json'),
  // Compaction without a document.
  yjsReadsGoMergedUpdates: view(load('go_merged.bin')) === expected,
  stateVectorsEqual: stateVector(bytes('go_sv.bin')) === stateVector(Y.encodeStateVector(reference)),
};

// An edit made by Go reaches a yjs replica that already has everything else.
Y.applyUpdate(reference, bytes('go_state_after_edit.bin'));
checks.yjsAppliesGoEdit = view(reference) === goProjection('go_after_edit.json');

// A yjs replica keeps editing on top of a Go-produced state and still converges with another yjs replica.
const peer = load('go_state_after_edit.bin');

peer.getMap('blocks').get('go-block').get('text').insert(0, 'js after go', { italic: true });
Y.applyUpdate(reference, Y.encodeStateAsUpdate(peer, Y.encodeStateVector(reference)));
checks.yjsEditsOnGoState = view(reference) === view(peer);

const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);

console.log(
  JSON.stringify({
    ok: failed.length === 0,
    failed,
    // Not an error: formatting that depends on the delivery order until the replicas exchange full state.
    orderDependentFormatting: view(sameOrder) !== expected,
    bytes: {
      yjsV1: bytes('js_state.bin').length,
      goV1: bytes('go_state.bin').length,
      goV2: bytes('go_state_v2.bin').length,
    },
  }),
);

process.exit(failed.length === 0 ? 0 : 1);
