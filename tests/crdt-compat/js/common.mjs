// Shared helpers: the document layout under test and its projection to plain JSON.
//
// Layout (the draft of the note format, see docs/collab-architecture.md):
//   blocks: Y.Map<blockId, Y.Map{ flavour, place: {p, k}, props: Y.Map, text: Y.Text }>
import * as Y from 'yjs';

export { Y };

export const sortKeys = (value) =>
  Array.isArray(value)
    ? value.map(sortKeys)
    : value && typeof value === 'object'
      ? Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortKeys(value[key])]))
      : value;

// Y.Text.toDelta() may return neighbouring runs with equal attributes as separate entries, and the Go port
// merges them. Both describe the same text, so runs are merged before comparing.
export function normalizeDelta(delta) {
  const runs = [];

  for (const op of delta) {
    const attributes = JSON.stringify(sortKeys(op.attributes ?? {}));
    const last = runs.at(-1);

    if (last && last.attributes === attributes && typeof op.insert === 'string') last.insert += op.insert;
    else runs.push({ insert: op.insert, attributes });
  }

  return runs.map((run) => ({ insert: run.insert, attributes: JSON.parse(run.attributes) }));
}

export function normalizeProjection(projection) {
  for (const id of Object.keys(projection)) projection[id].delta = normalizeDelta(projection[id].delta);

  return sortKeys(projection);
}

export function project(doc) {
  const out = {};

  doc.getMap('blocks').forEach((block, id) => {
    out[id] = {
      flavour: block.get('flavour'),
      place: block.get('place'),
      props: block.get('props').toJSON(),
      delta: block.get('text').toDelta(),
    };
  });

  return normalizeProjection(out);
}

export const canonical = (projection) => JSON.stringify(projection);

export function seededRandom(seed) {
  let state = seed >>> 0;

  return () => (state = (state * 1664525 + 1013904223) >>> 0) / 2 ** 32;
}
