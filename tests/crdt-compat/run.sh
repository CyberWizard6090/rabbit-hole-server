#!/usr/bin/env bash
# Runs the compatibility check: ./run.sh [number of seeds, default 10]
set -euo pipefail

cd "$(dirname "$0")"
seeds="${1:-10}"

(cd js && npm ci --silent)
(cd go && go build -o ../out/crdt-compat .)

failed=0
check() { # mode, steps, seed
  node js/gen.mjs "$3" "$2"
  ORDER="$1" ./out/crdt-compat
  if result="$(node js/verify.mjs)"; then echo "ok   $1 seed=$3 $result"; else echo "FAIL $1 seed=$3 $result"; failed=1; fi
}

mkdir -p out
for seed in $(seq 1 "$seeds"); do
  check inorder 3000 "$seed"
  # yjs itself is slow when thousands of updates arrive out of order, so the shuffled run is shorter.
  check shuffle 600 "$seed"
done

exit "$failed"
