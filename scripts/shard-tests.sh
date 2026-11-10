#!/usr/bin/env bash
# Prints a `go test -run` pattern that selects one shard of the tests of a package, so a slow suite can run as
# several parallel CI jobs: scripts/shard-tests.sh ./tests/integration <shard 1..N> <N>
#
# Tests are dealt out in turn from the sorted list of names, so every shard gets a similar number of them and the
# split does not depend on file layout. Every test belongs to exactly one shard; the script fails when the package
# has none.
set -euo pipefail

package=${1:?package, for example ./tests/integration}
shard=${2:?shard number from 1}
shards=${3:?number of shards}

if ! [[ $shard =~ ^[0-9]+$ && $shards =~ ^[0-9]+$ ]] || (( shard < 1 || shard > shards )); then
  echo "shard must be a number from 1 to $shards" >&2
  exit 2
fi

# Read the names from the source: `go test -list` would run TestMain, which needs the database.
names=$(grep -hoE '^func Test[A-Za-z0-9_]*\(' "$package"/*_test.go | sed -E 's/^func //; s/\($//' | grep -vx 'TestMain' | sort -u)
[[ -n $names ]] || { echo "no tests found in $package" >&2; exit 1; }

pattern=$(printf '%s\n' "$names" | awk -v shard="$shard" -v shards="$shards" '
  (NR - 1) % shards == shard - 1 { picked = picked (picked == "" ? "" : "|") $0 }
  END { if (picked == "") exit 3; printf "^(%s)$", picked }')
printf '%s\n' "$pattern"
