#!/usr/bin/env bash
# Runs every ablation row on the same synthetic utterances inside the enforced container limits, then
# writes results/ablation_summary.md. Usage: tools/run_ablations.sh [N utterances] [data dir]
set -uo pipefail
cd "$(dirname "$0")/.."
N="${1:-40}"; DATA="${2:-data/recordings/synth}"
mkdir -p results/runs
for cfg in config/ablations/*.yaml; do
  name=$(basename "$cfg" .yaml)
  [ -s "results/runs/abl_${name}.jsonl" ] && { echo "skip $name (done)"; continue; }
  find results -maxdepth 1 -name "${name}-*.jsonl" -delete
  echo "$(date +%H:%M:%S) running $name"
  CPUS=2 MEM=2g docker/run.sh bin/edgevoice -config "$cfg" -replay "$DATA" -n "$N" > "results/replay_${name}.log" 2>&1
  mv results/${name}-*.jsonl "results/runs/abl_${name}.jsonl"
done
{
  echo "# Ablation results ($N synthetic utterances, 2 CPU / 2 GB / no network)"
  echo
  go run ./cmd/harness -labels "$DATA/labels.jsonl" $(ls results/runs/abl_*.jsonl | sort)
} > results/ablation_summary.md
echo ABLATIONS_DONE
