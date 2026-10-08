#!/usr/bin/env bash
# Live graceful-degradation demo: tightens the running container's enforced limits step by step.
# Run in a second terminal while ./start.sh is running. Watch the Tier badge on the dashboard.
#   host/demo_degrade.sh            # 2 CPU/2 GB → 1.5 GB → 1 CPU/1 GB → 768 MB (commands only) → back
set -euo pipefail
NAME=edgevoice
step() {
  echo; echo "▶ $1"
  docker update --cpus "$2" --memory "$3" --memory-swap "$3" "$NAME" >/dev/null
  docker exec "$NAME" sh -c 'echo "  cpu.max=$(cat /sys/fs/cgroup/cpu.max)  memory.max=$(cat /sys/fs/cgroup/memory.max)"' 2>/dev/null || true
  echo "  (wait ~3 s for the tier switch, then speak a command and a question)"
  read -r -p "  press Enter for the next step… " _
}
docker inspect "$NAME" >/dev/null 2>&1 || { echo "container '$NAME' is not running: start ./start.sh first"; exit 1; }
step "T1: 2 CPU / 1.5 GB  (smaller LLM context)"            2 1536m
step "T2: 1 CPU / 1 GB    (LLM on 1 thread, tiny context)"   1 1024m
step "T3: 1 CPU / 768 MB  (LLM off; commands + cached clips)" 1 768m
step "Back to T0: 2 CPU / 2 GB"                               2 2048m
echo "done"
