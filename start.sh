#!/usr/bin/env bash
# EdgeVoice launcher: one command to bring up the whole demo.
#
#   ./start.sh                      # auto-picks a headset/USB mic if present, 2 CPU / 2 GB
#   ./start.sh --mic 1 --gain 3     # choose mic (see --list) and boost a quiet one
#   ./start.sh --cpus 1 --mem 1g    # smaller enforced limits (degradation demo)
#   ./start.sh --feed a.wav,b.wav   # backup demo: play WAV files instead of the mic
#   ./start.sh --duplex             # full-duplex: interrupt it mid-answer (use earphones)
#   ./start.sh --list               # list microphones
#
# Checks (and fixes) everything first: Docker running, container image, models, binaries, reply clips.
# Then starts the container (--network=none, enforced limits), the mic/speaker bridge and the dashboard.
set -euo pipefail
cd "$(dirname "$0")"

MIC="" GAIN=1 CPUS=2 MEM=2g FEED="" LIST=0 OPEN=true DUPLEX=false FEEDGAP=4s
while [ $# -gt 0 ]; do
  case "$1" in
    --mic) MIC="$2"; shift 2 ;;
    --gain) GAIN="$2"; shift 2 ;;
    --cpus) CPUS="$2"; shift 2 ;;
    --mem) MEM="$2"; shift 2 ;;
    --feed) FEED="$2"; shift 2 ;;
    --list) LIST=1; shift ;;
    --no-open) OPEN=false; shift ;;
    --duplex) DUPLEX=true; shift ;;
    --feedgap) FEEDGAP="$2"; shift 2 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown option: $1 (try --help)"; exit 1 ;;
  esac
done

say() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*"; exit 1; }
export GOPROXY=direct

command -v go >/dev/null || die "Go is not installed (brew install go)"
command -v docker >/dev/null || die "Docker is not installed (install Docker Desktop)"

# host bridge (fast native build; also used for --list)
go build -o bin/audiobridge-host ./cmd/audiobridge || die "could not build the audio bridge"
if [ "$LIST" = 1 ]; then bin/audiobridge-host -list; exit 0; fi

# 1. Docker running
if ! docker info >/dev/null 2>&1; then
  say "Starting Docker Desktop…"
  open -a Docker
  for _ in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 3; done
  docker info >/dev/null 2>&1 || die "Docker did not start; open Docker Desktop manually and retry"
fi

# 2. Container image (CPU-only llama.cpp + Go toolchain)
if ! docker image inspect edgevoice:dev >/dev/null 2>&1; then
  say "Building the container image (first time only, ~10 min)…"
  make image
fi

# 3. Models
need=(models/silero_vad.onnx models/zipformer-en/tokens.txt models/indicconformer-ta/model.int8.onnx
      models/vits-piper-en_US-amy-low models/piper-ta/tokens.txt models/llm/Qwen3-0.6B-Q4_K_M.gguf)
missing=0
for f in "${need[@]}"; do [ -e "$f" ] || { echo "   missing: $f"; missing=1; }; done
if [ "$missing" = 1 ]; then
  say "Downloading models (needs internet, one time)…"
  bash tools/download_models.sh
  for f in "${need[@]}"; do [ -e "$f" ] || die "still missing $f; see README 'Setup'"; done
fi

# 4. Container binaries (rebuild if any Go source is newer)
if [ ! -x bin/edgevoice ] || [ -n "$(find cmd internal -name '*.go' -newer bin/edgevoice -print -quit)" ] \
   || [ -n "$(find internal -name '*.yaml' -newer bin/edgevoice -print -quit)" ]; then
  say "Building EdgeVoice inside the container…"
  make build >/dev/null
fi

# 5. Reply clips (rebuild if templates changed)
if [ ! -f clips/manifest.json ] || [ internal/reply/templates.yaml -nt clips/manifest.json ] \
   || [ internal/reply/tables.go -nt clips/manifest.json ]; then
  say "Pre-synthesizing reply clips (~2 min)…"
  make clips >/dev/null
fi

# 6. Microphone: prefer a headset/USB mic over the built-in one unless --mic was given
if [ -z "$MIC" ] && [ -z "$FEED" ]; then
  MIC=$(bin/audiobridge-host -list | grep -viE 'MacBook|Built-in|Microphone \(default\)' | head -1 | cut -d: -f1 || true)
  [ -z "$MIC" ] && MIC=-1
fi
[ -z "$MIC" ] && MIC=-1

# free the name in case a previous run was killed
docker rm -f edgevoice >/dev/null 2>&1 || true

say "Starting EdgeVoice: ${CPUS} CPU / ${MEM} RAM / no network"
if [ -n "$FEED" ]; then
  say "Input: WAV feed ($FEED)"
else
  say "Microphone: $(bin/audiobridge-host -list | grep "^${MIC}:" || echo 'system default')"
fi
say "Dashboard: http://localhost:8080  ·  say \"Hey Computer\" then your command  ·  Ctrl-C to stop"
echo
args=(-cpus "$CPUS" -mem "$MEM" -mic "$MIC" -gain "$GAIN" -open="$OPEN" -duplex="$DUPLEX" -feedgap "$FEEDGAP")
[ "$DUPLEX" = true ] && say "Full-duplex ON: talk over it to interrupt (use earphones; laptop speakers can self-interrupt)"
[ -n "$FEED" ] && args+=(-feed "$FEED")
exec bin/audiobridge-host "${args[@]}"
