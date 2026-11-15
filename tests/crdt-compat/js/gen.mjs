// Three yjs replicas make random edits and sync now and then. Writes every incremental update, the merged
// state and the expected projection to out/.
//
//   node js/gen.mjs <seed> [steps]
import fs from 'node:fs';

import { Y, project, canonical, seededRandom } from './common.mjs';

const seed = Number(process.argv[2] ?? 1);
const steps = Number(process.argv[3] ?? 3000);
const random = seededRandom(seed);
const pick = (items) => items[Math.floor(random() * items.length)];

const docs = [101, 102, 103].map((clientID) => {
  const doc = new Y.Doc();

  doc.clientID = clientID + seed * 1000;

  return doc;
});
const updates = [];

for (const doc of docs) {
  doc.on('update', (update, origin) => {
    if (origin !== 'sync') updates.push(Buffer.from(update).toString('base64'));
  });
}

// Cyrillic, emoji with surrogate pairs and joiners, the object character Inkbrick uses for inline formulas.
const WORDS = ['привет ', 'hello ', '😀👩‍👩‍👧', '￼', 'x', 'мир\n', '𝒳 '];
const INSERT_ATTRIBUTES = [
  {},
  { bold: true },
  { italic: true, color: '#ff0000' },
  { link: 'https://example.com/a?b=1' },
  { noteRef: '6f1c' },
];
const FORMATS = [{ bold: true }, { bold: null }, { italic: true }, { color: '#00f' }, { link: null }];
const PROP_VALUES = [1, 2, 'center', true, null, { nested: [1, 'a'] }];

let created = 0;
const orderKey = () => 'a' + Math.floor(random() * 1e6).toString(36);

function addBlock(doc) {
  const blocks = doc.getMap('blocks');
  const block = new Y.Map();

  block.set('flavour', pick(['block:paragraph', 'block:heading', 'block:list']));
  block.set('place', { p: pick(['root', ...blocks.keys()]), k: orderKey() });
  block.set('props', new Y.Map());
  block.set('text', new Y.Text());
  blocks.set(`b${doc.clientID}-${created++}`, block);
}

// yjs replaces the halves of a split surrogate pair with U+FFFD on the replica that split it, and replicas
// then show different text. Offsets therefore never land inside a pair (the editor bridge must do the same).
function snap(text, index) {
  const value = text.toString();
  const code = value.charCodeAt(index);

  return index > 0 && index < value.length && code >= 0xdc00 && code <= 0xdfff ? index - 1 : index;
}

function range(text, maxLength) {
  const length = text.length;
  const from = snap(text, Math.floor(random() * length));
  const to = snap(text, Math.min(length, from + 1 + Math.floor(random() * maxLength)));

  return [from, to - from || 2];
}

function step(doc) {
  const blocks = doc.getMap('blocks');
  const ids = [...blocks.keys()];

  if (ids.length === 0 || random() < 0.08) return addBlock(doc);

  const block = blocks.get(pick(ids));
  const text = block.get('text');
  const roll = random();

  if (roll < 0.45) {
    const word = pick(WORDS);
    const attributes = word === '￼' ? { formula: 'x^2' } : pick(INSERT_ATTRIBUTES);

    text.insert(snap(text, Math.floor(random() * (text.length + 1))), word, { ...attributes });
  } else if (roll < 0.6 && text.length > 0) {
    text.delete(...range(text, 4));
  } else if (roll < 0.75 && text.length > 0) {
    text.format(...range(text, 6), pick(FORMATS));
  } else if (roll < 0.85) {
    block.set('place', { p: pick(['root', ...ids]), k: orderKey() });
  } else if (roll < 0.93) {
    block.get('props').set(pick(['level', 'align', 'checked']), pick(PROP_VALUES));
  } else if (roll < 0.96) {
    block.set('flavour', pick(['block:paragraph', 'block:quote']));
  } else if (ids.length > 3) {
    blocks.delete(pick(ids));
  }
}

function syncAll() {
  for (const from of docs) {
    for (const to of docs) {
      if (from !== to) Y.applyUpdate(to, Y.encodeStateAsUpdate(from, Y.encodeStateVector(to)), 'sync');
    }
  }
}

for (let i = 0; i < steps; i++) {
  step(pick(docs));

  if (random() < 0.02) syncAll();
}

syncAll();
syncAll();

const [first, ...rest] = docs.map((doc) => canonical(project(doc)));

if (rest.some((other) => other !== first)) throw new Error('the yjs replicas did not converge');

fs.mkdirSync('out', { recursive: true });
fs.writeFileSync('out/updates.json', JSON.stringify(updates));
fs.writeFileSync('out/expected.json', first);
fs.writeFileSync('out/js_state.bin', Y.encodeStateAsUpdate(docs[0]));
