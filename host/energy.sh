#!/usr/bin/env bash
# Energy per turn (PRD §8): samples macOS CPU power with powermetrics while the SAME utterances are
# replayed through the baseline and the full system inside the enforced container, subtracts the idle
# baseline, and reports joules per turn. Needs your password once (powermetrics requires sudo).
#
#   host/energy.sh            # 20 utterances per config (≈ 10 min total)
#   host/energy.sh 10         # quicker
# Close other heavy apps first: powermetrics measures the whole Mac.
set -euo pipefail
cd "$(dirname "$0")/.."
N="${1:-20}"
OUT=results/energy; mkdir -p "$OUT"
sudo -v || { echo "needs sudo for powermetrics"; exit 1; }
( while true; do sudo -n true; sleep 50; done ) & KEEP=$!   # keep sudo alive
trap 'kill $KEEP 2>/dev/null || true' EXIT

# mean CPU power (mW) and duration (s) from a powermetrics log (200 ms samples)
stats() { awk '/^CPU Power:/ {s+=$3; n++} END {if (n) printf "%.1f %.1f\n", s/n, n*0.2; else print "0 0"}' "$1"; }

echo "▶ idle baseline (30 s, container not running)…"
sudo powermetrics --samplers cpu_power -i 200 -n 150 > "$OUT/idle.txt" 2>/dev/null
read -r IDLE_MW _ <<<"$(stats "$OUT/idle.txt")"
echo "  idle CPU power: ${IDLE_MW} mW"

echo "config,turns,seconds,mean_cpu_mW,idle_mW,net_J,J_per_turn" > "$OUT/energy.csv"
for cfg in 01_baseline 10_full; do
  echo "▶ $cfg ($N utterances)…"
  find results -maxdepth 1 -name "${cfg}-*.jsonl" -delete
  sudo powermetrics --samplers cpu_power -i 200 > "$OUT/$cfg.txt" 2>/dev/null & PM=$!
  CPUS=2 MEM=2g docker/run.sh bin/edgevoice -config "config/ablations/$cfg.yaml" -replay data/recordings/synth -n "$N" > "$OUT/$cfg.log" 2>&1
  sudo kill -INT $PM 2>/dev/null || true; wait $PM 2>/dev/null || true
  TURNS=$(cat results/${cfg}-*.jsonl | grep -c '"turn"' || true)
  mv results/${cfg}-*.jsonl "$OUT/${cfg}.jsonl"
  read -r MW SECS <<<"$(stats "$OUT/$cfg.txt")"
  NETJ=$(awk -v m="$MW" -v i="$IDLE_MW" -v s="$SECS" 'BEGIN {printf "%.1f", (m-i)/1000*s}')
  JPT=$(awk -v j="$NETJ" -v t="$TURNS" 'BEGIN {if (t) printf "%.2f", j/t; else print "nan"}')
  echo "$cfg,$TURNS,$SECS,$MW,$IDLE_MW,$NETJ,$JPT" >> "$OUT/energy.csv"
  echo "  $TURNS turns, ${SECS}s, mean ${MW} mW → ${NETJ} J above idle → ${JPT} J/turn"
done
echo; column -s, -t < "$OUT/energy.csv"
echo "saved: $OUT/energy.csv"
