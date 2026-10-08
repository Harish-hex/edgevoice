#!/usr/bin/env bash
# Usage: CPUS=2 MEM=2g docker/run.sh <cmd...>
# Enforced limits (no swap) and NO network. Go modules are mounted read-only from the host cache.
set -euo pipefail
cd "$(dirname "$0")/.."
TTY=""; [ -t 0 ] && [ -t 1 ] && TTY="-t"
exec docker run --rm -i $TTY ${NAME:+--name "$NAME"} \
  --cpus="${CPUS:-2}" --memory="${MEM:-2g}" --memory-swap="${MEM:-2g}" \
  --network=none \
  -v "$PWD":/src -v "$PWD/models":/models:ro -v "$PWD/sock":/sock \
  -v "$(go env GOMODCACHE)":/go/pkg/mod:ro -v edgevoice-gocache:/root/.cache/go-build \
  edgevoice:dev "$@"
