# EdgeVoice Implementation Plan

> **For Claude:** Execute task-by-task (skill: `antigravity-awesome-skills:executing-plans` or
> `subagent-driven-development`). Read `PRDcbt.md` and `docs/DESIGN.md` first. The user is **not
> comfortable debugging Docker/infra** — drive setup, builds and debugging yourself; only stop for
> things needing their hands (start Docker Desktop if it won't launch, `sudo` for powermetrics, mic
> permission, recording audio).

**Goal:** A fully offline, CPU-only English + Tanglish voice assistant in a Docker container capped at
2 cores / 2 GB, beating a Whisper→LLM→Piper baseline on latency and footprint, with measured ablations.

**Architecture:** One Go binary (`edgevoice`) runs the streaming pipeline (VAD → zipformer ASR →
normalizer → rule parser → clips | llama-server → clause TTS) inside a `--network=none` container;
sherpa-onnx Go bindings do VAD/ASR/TTS on CPU, `llama-server` is a supervised subprocess. A host Go
`audiobridge` moves 16 kHz PCM between mic/speaker and the container.

**Tech stack:** Go ≥1.22 (host has 1.26.1), sherpa-onnx Go (`github.com/k2-fsa/sherpa-onnx-go`),
llama.cpp `llama-server` (CPU build), `gopkg.in/yaml.v3`, `github.com/gen2brain/malgo`, Docker Desktop,
Python 3 only in `tools/`.

**Verified reference code:** `docs/plans/reference/` holds the pure-Go `iface` + `nlu` packages,
already compiled and passing 28 seed/messy-ASR cases (`go test` green on 2026-10-08). Phase 2 tasks
TDD them into the repo from there.

**Environment quirks found during planning:**
- `proxy.golang.org` fails TLS on this Mac; direct fetch works → `go env -w GOPROXY=direct`.
  If `sum.golang.org` also fails, set `GOSUMDB=off` **only** for this project's Makefile env, and note it.
- Docker daemon not running at plan time → Task 1.1 starts it.
- The same TLS interception may affect `docker build` downloads → Task 1.3 has a fallback (download on
  host into `third_party/`, `COPY` in).

---

## Skills to load per phase

| Phase | Skills |
|---|---|
| All | `agent-protocols:incremental-implementation`, `agent-protocols:karpathy-guidelines` (simplicity), `antigravity-awesome-skills:verification-before-completion` before claiming any gate |
| 1 Infra gate | `antigravity-awesome-skills:docker-expert`, `antigravity-awesome-skills:systematic-debugging` |
| 2 Pure NLU / reply / actions | `agent-protocols:test-driven-development`, `antigravity-awesome-skills:golang-pro` |
| 3 English loop | `antigravity-awesome-skills:go-concurrency-patterns`, `antigravity-awesome-skills:voice-ai-engine-development`, `antigravity-awesome-skills:local-llm-expert` |
| 4 Clips + Tanglish + harness | `agent-protocols:test-driven-development`, `agent-protocols:performance-optimization` |
| 5 Baseline / ablation / degradation | `agent-protocols:performance-optimization`, `antigravity-awesome-skills:local-llm-expert` |
| 6 Numbers + write-up | `agent-protocols:documentation-and-adrs`, `antigravity-awesome-skills:avoid-ai-writing` |
| Any failure | `antigravity-awesome-skills:systematic-debugging` (root cause before fixes) |

## Timeline (hours from event start) and cut list

| Hours | Phase | Exit gate |
|---|---|---|
| 0–0.5 | 0 Bootstrap | `make test` runs (0 tests ok) |
| 0.5–2 | 1 Infra gate (downloads/builds in background) | `make gate` PASS — **must pass by h2** |
| (during waits) | 2 Pure NLU/reply/actions | `make test` green |
| 2–8 | 3 English loop + LLM path | Speak English command & chat → hear reply, metrics JSONL written |
| 8–14 | 4 Clips, Tanglish, synth data, harness | **h14 hard gate: Tanglish e2e works, else freeze Tanglish scope** |
| 14–18 | 5 Baseline, ablations, degradation | `results/summary.md` has all rows; tier switch demo works |
| 18–21 | 6 Energy, real recordings, write-up | Numbers final |
| 21–23 | 7 Rehearsal | Wi-Fi-off dry run ×3 |

**Cut list (drop in this order if behind):** O5 speculative prefill → keyword spotter (VAD + push-to-talk
only) → leave-one-out ablations → D13 script study → quantization sweep → T2 tier (keep T0/T1/T3) →
Option A Tamil ASR upgrade. **Never cut:** gate, latency metric, baseline, command fast path, clips,
enforced limits, T3.

---

## Phase 0 — Bootstrap (h0–0.5)

### Task 0.1: Repo skeleton

**Files:** Create `go.mod`, `Makefile`, `.gitignore`, `CLAUDE.md`, `.claude/launch.json` (not needed now; skip).

**Step 1:** Init.
```bash
cd /Users/harishharish/edgevoice
git init -b main
go env -w GOPROXY=direct
go mod init edgevoice
go get gopkg.in/yaml.v3@v3.0.1
mkdir -p cmd/{edgevoice,audiobridge,harness,buildclips,synthdata,record,gate} \
  internal/{iface,config,metrics,audio,asr,nlu,actions,reply,llm,tts,degrade,pipeline} \
  config/{ablations,tiers} docker host tools clips data/recordings/{synth,real} results models sock third_party
```
Expected: `go.mod` with `module edgevoice`, `go 1.2x`, yaml.v3 required.

**Step 2:** `.gitignore`
```
models/
clips/*.pcm
data/recordings/**/*.wav
results/*.jsonl
sock/
third_party/
/bin/
```

**Step 3:** `CLAUDE.md`
```markdown
# EdgeVoice
Read `PRDcbt.md` (spec), then `docs/DESIGN.md` (confirmed overrides — these win), then
`docs/plans/2026-10-08-edgevoice-implementation.md` (task plan). Runtime is Go; Python only in tools/.
Never add network calls to the runtime or enable GPU/Metal/CoreML. Pure packages (nlu, reply, actions)
must not import cgo/model code. Use `make test` natively; cgo code builds only in the container (`make dev`).
GOPROXY=direct (proxy.golang.org TLS fails on this machine).
```

**Step 4:** `Makefile` (grows over phases; start with):
```make
PURE := ./internal/iface/... ./internal/nlu/... ./internal/reply/... ./internal/actions/... ./internal/config/...
CPUS ?= 2
MEM  ?= 2g
export GOPROXY=direct

test:
	go vet $(PURE) && go test $(PURE)

.PHONY: test
```

**Step 5:** Run `make test` → expect `no packages to test`/ok. Commit:
```bash
git add -A && git commit -m "chore: bootstrap edgevoice repo"
```

---

## Phase 1 — Infra gate (h0.5–2) — MUST PASS

Kick off long-running work first (Tasks 1.1–1.3), then do Phase 2 while they run.

### Task 1.1: Start Docker Desktop

```bash
open -a Docker
until docker info >/dev/null 2>&1; do sleep 3; done   # use Monitor tool with an until-loop, not sleep
docker info --format '{{.OperatingSystem}} cpus={{.NCPU}} mem={{.MemTotal}}'
```
Expected: `Docker Desktop ... cpus=N`. Docker Desktop VM must have ≥4 CPUs / ≥4 GB (Settings →
Resources) so `--cpus=2 --memory=2g` is a real sub-limit. If it can't be launched, ask the user.

### Task 1.2: Model download script (background)

**Files:** Create `tools/download_models.sh` — the ONLY internet-touching script.

```bash
#!/usr/bin/env bash
# Downloads all models into ./models and records SHA256s. Only script allowed to use the network.
set -euo pipefail
cd "$(dirname "$0")/../models"
GH=https://github.com/k2-fsa/sherpa-onnx/releases/download
HF=https://huggingface.co
fetch() { [ -e "$2" ] || curl -fL --retry 3 -o "$2" "$1"; }
untar() { [ -d "${1%.tar.bz2}" ] || tar xjf "$1"; }

fetch $GH/asr-models/silero_vad.onnx silero_vad.onnx
for m in sherpa-onnx-streaming-zipformer-en-2023-06-26 sherpa-onnx-whisper-base; do
  fetch $GH/asr-models/$m.tar.bz2 $m.tar.bz2; untar $m.tar.bz2; done
fetch $GH/tts-models/vits-piper-en_US-amy-low.tar.bz2 vits-piper-en_US-amy-low.tar.bz2; untar vits-piper-en_US-amy-low.tar.bz2
fetch $GH/kws-models/sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01.tar.bz2 kws.tar.bz2; tar xjf kws.tar.bz2
# MMS Tamil: try a prebuilt sherpa export first; else Task 4.1 converts it.
fetch $GH/tts-models/vits-mms-tam.tar.bz2 vits-mms-tam.tar.bz2 && untar vits-mms-tam.tar.bz2 || echo "WARN: no prebuilt vits-mms-tam; convert in Task 4.1"
mkdir -p llm && cd llm
fetch $HF/unsloth/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf Qwen3-0.6B-Q4_K_M.gguf
fetch $HF/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf Llama-3.2-1B-Instruct-Q4_K_M.gguf
fetch $HF/unsloth/gemma-3-1b-it-GGUF/resolve/main/gemma-3-1b-it-Q4_K_M.gguf gemma-3-1b-it-Q4_K_M.gguf
cd .. && find . -type f \( -name '*.onnx' -o -name '*.gguf' -o -name 'tokens.txt' \) -exec shasum -a 256 {} \; > manifest.txt
echo DONE
```
Run in background (`run_in_background: true`): `bash tools/download_models.sh`. Verify each URL's
exact filename against the release page if a 404 appears (names drift); fix the script, not by hand.
Gemma may require HF licence acceptance — if 403, drop it from the bake-off (Qwen3 + Llama suffice).

### Task 1.3: Dockerfile (CPU-only) + run.sh

**Files:** Create `docker/Dockerfile`, `docker/run.sh`, `.dockerignore` (`models/ data/ results/ clips/*.pcm .git`).

`docker/Dockerfile`:
```dockerfile
# Stage 1: llama-server, CPU only (no Metal exists in Linux; also disable BLAS/GPU backends explicitly)
FROM debian:bookworm-slim AS llama
RUN apt-get update && apt-get install -y --no-install-recommends build-essential cmake git ca-certificates libcurl4-openssl-dev && rm -rf /var/lib/apt/lists/*
ARG LLAMA_TAG=master
RUN git clone --depth 1 --branch ${LLAMA_TAG} https://github.com/ggml-org/llama.cpp /llama
WORKDIR /llama
RUN cmake -B build -DGGML_METAL=OFF -DGGML_BLAS=OFF -DGGML_CUDA=OFF -DGGML_VULKAN=OFF \
      -DLLAMA_CURL=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON \
      -DCMAKE_BUILD_TYPE=Release && cmake --build build --target llama-server -j

# Stage 2: Go toolchain + runtime (cgo for sherpa-onnx)
FROM golang:1.23-bookworm AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends procps && rm -rf /var/lib/apt/lists/*
COPY --from=llama /llama/build/bin/llama-server /usr/local/bin/llama-server
COPY --from=llama /llama/build/bin/*.so* /usr/local/lib/
RUN ldconfig
ENV GOPROXY=direct CGO_ENABLED=1 GOFLAGS=-buildvcs=false
WORKDIR /src
# Pre-fetch sherpa-onnx Go (ships prebuilt aarch64 .so) so --network=none runs need nothing
COPY go.mod go.sum ./
RUN go mod download
CMD ["bash"]
```
(Before building, run `go get github.com/k2-fsa/sherpa-onnx-go@latest` on the host so go.sum has it.)

`docker/run.sh`:
```bash
#!/usr/bin/env bash
# Usage: CPUS=2 MEM=2g docker/run.sh <cmd...>   — enforced limits, no network, no swap.
set -euo pipefail
cd "$(dirname "$0")/.."
exec docker run --rm -i --name edgevoice \
  --cpus="${CPUS:-2}" --memory="${MEM:-2g}" --memory-swap="${MEM:-2g}" \
  --network=none \
  -v "$PWD":/src -v "$PWD/models":/models:ro -v "$PWD/sock":/sock \
  -v edgevoice-gocache:/root/.cache/go-build -v edgevoice-gomod:/go/pkg/mod \
  edgevoice:dev "$@"
```
Makefile additions:
```make
image:
	docker build -f docker/Dockerfile -t edgevoice:dev .
dev:
	CPUS=$(CPUS) MEM=$(MEM) docker/run.sh bash
limits:
	CPUS=$(CPUS) MEM=$(MEM) docker/run.sh sh -c 'cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max; ip link 2>/dev/null || ls /sys/class/net'
```
Run `make image` in background. **If git clone / go mod download fail with TLS errors inside build:**
clone llama.cpp on host into `third_party/llama.cpp` and `COPY` it; for Go modules run
`go mod vendor` on host and build with `-mod=vendor`.

Verify: `make limits` → `200000 100000` and `2147483648`; network list shows only `lo`.
Commit.

### Task 1.4: Audio transport + bridge loopback

**Files:** Create `internal/audio/frame.go` (+ `_test.go`), `cmd/audiobridge/main.go`, `cmd/gate/echo.go`.

Wire format (both directions): `[1 byte type][4 byte LE length][payload]`; type 0x01 = PCM int16 LE
16 kHz mono, 0x02 = control (`"ptt_down"`, `"ptt_up"`), 0x03 = text status line (container→host only).

**Step 1 (TDD, pure):** `frame_test.go` — round-trip `WriteFrame`/`ReadFrame` for each type and a
truncated stream (expect `io.ErrUnexpectedEOF`). Implement with `encoding/binary`. `make test` green
(add `./internal/audio/frame.go` to PURE via a `frame` subpackage: `internal/audio/wire`).

**Step 2:** `cmd/audiobridge` (host, malgo): duplex device 16 kHz mono S16; capture callback → `WriteFrame(PCM)`;
reader goroutine → jitter buffer (ring, 200 ms) → playback callback. Spacebar toggles PTT control
frames (raw terminal mode). Transport flag `-transport=sock|stdio`:
- `sock`: connect to `./sock/audio.sock` (container listens).
- `stdio`: bridge spawns `docker/run.sh edgevoice ...` itself and uses its stdin/stdout; container
  logs go to stderr. **This keeps `--network=none` even if sockets fail.**

**Step 3:** `cmd/gate echo` mode: container reads frames and writes the same PCM back (loopback).
Test: `go run ./cmd/audiobridge -transport=sock` + `docker/run.sh go run ./cmd/gate echo` → speak, hear
yourself ~200 ms later. **If no connection within 10 min of debugging, switch default to `stdio`** and
record it in `docs/DESIGN.md` decision log (#5). Commit.

### Task 1.5: Gate program (`make gate`)

**Files:** Create `cmd/gate/main.go`.

Inside the container: (1) Silero VAD on `models/.../test_wavs/0.wav` → prints speech segments;
(2) streaming zipformer → prints transcript; (3) Piper synth "gate passed" → writes `/src/results/gate.wav`;
(4) spawn `llama-server -m /models/llm/Qwen3-0.6B-Q4_K_M.gguf --threads 2 --ctx-size 512 --host 127.0.0.1 --port 8081`,
poll `GET /health` until 200 (timeout 60 s), one `/completion` of 8 tokens; (5) print `cpu.max`,
`memory.max`, `memory.peak`. Exit 0 only if all pass, printing `GATE PASS`.

sherpa-onnx Go API (verify names against `github.com/k2-fsa/sherpa-onnx/tree/master/go-api-examples`):
```go
import sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

vc := sherpa.VadModelConfig{SampleRate: 16000, NumThreads: 1, Provider: "cpu"}
vc.SileroVad = sherpa.SileroVadModelConfig{Model: "/models/silero_vad.onnx", Threshold: 0.5,
    MinSilenceDuration: 0.1, MinSpeechDuration: 0.1, WindowSize: 512}
vad := sherpa.NewVoiceActivityDetector(&vc, 30) // seconds of buffer
defer sherpa.DeleteVoiceActivityDetector(vad)

rc := sherpa.OnlineRecognizerConfig{}
rc.FeatConfig = sherpa.FeatureConfig{SampleRate: 16000, FeatureDim: 80}
d := "/models/sherpa-onnx-streaming-zipformer-en-2023-06-26/"
rc.ModelConfig.Transducer = sherpa.OnlineTransducerModelConfig{
    Encoder: d + "encoder-epoch-99-avg-1-chunk-16-left-128.int8.onnx",
    Decoder: d + "decoder-epoch-99-avg-1-chunk-16-left-128.onnx",
    Joiner:  d + "joiner-epoch-99-avg-1-chunk-16-left-128.int8.onnx"}
rc.ModelConfig.Tokens, rc.ModelConfig.NumThreads, rc.ModelConfig.Provider = d+"tokens.txt", 1, "cpu"
rc.DecodingMethod, rc.MaxActivePaths = "modified_beam_search", 4
rec := sherpa.NewOnlineRecognizer(&rc); stream := sherpa.NewOnlineStream(rec)
stream.AcceptWaveform(16000, samples); stream.InputFinished()
for rec.IsReady(stream) { rec.Decode(stream) }
fmt.Println(rec.GetResult(stream).Text)

tc := sherpa.OfflineTtsConfig{}
p := "/models/vits-piper-en_US-amy-low/"
tc.Model.Vits = sherpa.OfflineTtsVitsModelConfig{Model: p + "en_US-amy-low.onnx", Tokens: p + "tokens.txt", DataDir: p + "espeak-ng-data"}
tc.Model.NumThreads, tc.Model.Provider = 1, "cpu"
tts := sherpa.NewOfflineTts(&tc); audio := tts.Generate("gate passed", 0, 1.0) // audio.Samples []float32
```
(Check exact model filenames with `ls models/<dir>` — fix paths in config, not code.)

Makefile: `gate: ; CPUS=2 MEM=2g docker/run.sh go run ./cmd/gate`.
**Expected:** `GATE PASS`. If sherpa-onnx Go fails to load (missing .so, symbol errors) and can't be
fixed in ~45 min → **Python fallback**: stop, tell the user, re-plan with the same interfaces in Python.
Commit `feat: hour-2 gate passing`.

### Task 1.6: LLM bake-off (D5) — 15 min, background

**Files:** Create `tools/bench_llm.sh` (runs inside container), `tools/bench_prompts.txt` (10 prompts:
5 EN facts/chit-chat, 5 romanized Tanglish chat).
For each GGUF: start llama-server (`--threads 2 --ctx-size 1024`), measure TTFT and tok/s via `/completion`
with `stream:false` and the `timings` field, store answers. Write `results/llm_bench.md`. Pick T0 = best
quality with TTFT < 400 ms and ≥ 10 tok/s at 2 cores; T1/T2 = Qwen3-0.6B. For Qwen3 append `/no_think`
to the system prompt. Record decision in DESIGN log.

---

## Phase 2 — Pure logic via TDD (run during Phase 1 waits)

Each task: copy the **test** from `docs/plans/reference/` first → `make test` fails → copy/write the
implementation → `make test` passes → commit. Do not skip the red step.

### Task 2.1: `internal/iface/iface.go`
Copy `docs/plans/reference/internal/iface/iface.go`. `go build ./internal/iface` ok. Commit.

### Task 2.2: Fuzzy primitives
Test: `reference/internal/nlu/fuzzy_test.go` → `go test ./internal/nlu -run 'Levenshtein|Phonetic'`
FAIL (undefined) → copy `fuzzy.go` → PASS. Commit `feat(nlu): levenshtein + phonetic key`.

### Task 2.3: Lexicon + translit + normalizer + langmode
Test: `normalize_test.go` → FAIL → copy `lexicon.yaml`, `lexicon.go`, `translit.go`, `normalize.go`,
`langmode.go` → PASS. Commit.

### Task 2.4: Slots + intents
Test: `intents_test.go` (28 cases incl. messy ASR) → FAIL → copy `slots.go`, `intents.go` → PASS.
Then **add ≥30 more messy variants** (spellings, word orders, English-ASR splits like "pan new",
"nali key", "tie mer") and fix the lexicon (prefer adding variants over code changes). Commit.

### Task 2.5: `internal/actions`
**Files:** `actions.go`, `actions_test.go`. `type State struct{ Alarms []time.Time; Timers []time.Time; Reminders []Reminder }`
and `func Execute(st *State, in *iface.Intent, now time.Time) (Result, error)` where
`Result{TemplateID string; Slots map[string]string}`. Cases: alarm.set appends + returns
`alarm.set` with `hour12`, `period` (am/pm/night), `day`; alarm.cancel clears (`alarm.cancel.none` if empty);
timer.set; clock.time/date fill `hour12`,`minute`,`period`/`date`; calc evaluates `a op b` (integer,
`/` → one decimal; divide by zero → `calc.error`). Table tests first. Commit.

### Task 2.6: `internal/reply` templates
**Files:** `templates.yaml`, `templates.go`, `templates_test.go`.
Schema per `templateID` × mode: `display: "Seri, {day_ta} {period_ta} {hour} manikku alarm vechiten"`,
`speech: ["சரி,", "{day_ta}", "{period_ta}", "{hour_ta}", "மணிக்கு அலாரம் வெச்சிட்டேன்"]`;
EN: `speech: ["Done,", "alarm set for", "{hour}", "{period_en}", "{day_en}"]`.
Slot-value fragment tables: `hour_ta` 1–12 in Tamil script, `day_ta`, `period_ta`, numbers for minutes
5/10/15/20/30/45, offline/error/“quick commands only” lines in both modes.
Functions: `Display(id, mode, slots) string`, `Fragments(id, mode, slots) []string` (resolved fragment
texts — the clip keys), `AllFragments() []string` (for buildclips). Tests: every intent in nlu has
EN + TANGLISH templates; `AllFragments` has no unresolved `{…}`; seed alarm renders exactly as above.
Commit.

---

## Phase 3 — English loop end to end (h2–8)

### Task 3.1: Config
**Files:** `internal/config/config.go`, `config/default.yaml`, `config_test.go`.
Struct mirrors YAML: `Models{VAD, ASR{Encoder,Decoder,Joiner,Tokens,Hotwords}, TTSEn, TTSTa, LLMModels map[tier]path, Whisper}`,
`Endpoint{SilenceMs:400, EarlySilenceMs:200, MaxUtteranceMs:10000}`,
`Opt{O1StreamingASR, O2TunedEndpoint, O3Semantic, O4PromptCache, O5SpecPrefill, O6ClauseTTS, O7FastPath, O8Clips, Hotwords, Fuzzy, Merge bool}`,
`LLM{Threads, Ctx, MaxTokens:80, TimeoutMs:3000}`, `Transport: sock|stdio|replay`.
`Load(path) (*Config, error)` with `extends:` support (ablation files override `default.yaml`). Test:
ablation file overrides one flag only. Commit.

### Task 3.2: Metrics bus + cgroup/RSS readers
**Files:** `internal/metrics/bus.go`, `cgroup.go`, `rss.go`, tests with fixture files.
`Bus.Start(turnID)`, `Mark(turnID, name)` (offset `time.Since(processStart)`), `Set(turnID, key, any)`,
`End(turnID)` → derived `e2e_ms = t_first_audio_out − t_last_voiced` + stage deltas → JSONL line to
`results/run-<ts>.jsonl`. `cgroup.go` parses `cpu.max`, `memory.max`, `memory.current`, `memory.peak`,
`cpu.stat` (`usage_usec`) from a root dir (param, so tests use `testdata/`). `rss.go` parses
`/proc/<pid>/status` VmRSS/VmHWM. Sampler goroutine every 50 ms keeps per-turn peak. Pure → add to PURE. Commit.

### Task 3.3: VAD + endpointer
**Files:** `internal/audio/vad.go` (sherpa, cgo), `internal/audio/endpointer.go` (pure) + `endpointer_test.go`.
VAD wraps Silero with 512-sample windows (32 ms; note PRD said 30 ms) and emits SpeechStart/SpeechEnd with
`t_last_voiced` = end of last voiced window. Endpointer (pure, table-tested with synthetic event
sequences): fires at `silence ≥ SilenceMs`, or `≥ EarlySilenceMs` when `CompleteFn(partial)` is true
(O3; `CompleteFn` = parser returns non-nil intent), or at 10 s cap; records false cut-off if speech
resumes ≤ 600 ms after firing. Commit.

### Task 3.4: Streaming English ASR
**Files:** `internal/asr/english.go` implementing `iface.StreamingASR` (zipformer, modified beam search,
optional hotwords file generated from lexicon Tamil variants by `cmd/buildclips -hotwords`). Partial
after every Accept via `IsReady/Decode` loop; `Finalize` = `InputFinished` + drain + `Reset`.
Container test: `go test ./internal/asr -run Wav` on a test wav from the model dir. Commit.

### Task 3.5: TTS + PCM utils
**Files:** `internal/audio/pcm.go` (pure: float32↔int16, linear resample, crossfade(a,b,ms)) + tests;
`internal/tts/tts.go` (sherpa OfflineTts, Piper EN / MMS TA, resample to 16 kHz in pcm.go only);
`internal/tts/clause.go` (pure) + tests: split stream of tokens on `, . ? !` or 8 words; first clause
emitted ASAP. Commit.

### Task 3.6: Pipeline (command path with English)
**Files:** `internal/pipeline/pipeline.go`, `cmd/edgevoice/main.go`.
Stages as goroutines with buffered channels (see DESIGN §4.2): `in` (wire frames or replay WAV) →
VAD/endpointer + ASR.Accept → on endpoint: Finalize → Normalize → Parse → `actions.Execute` →
reply (for now: live TTS of `reply.Display` text) → `out`. Per-turn `context.Context`; barge-in
(SpeechStart while playing) cancels and flushes `out`. Panic recovery per turn → error clip/TTS.
Status line frames (type 0x03) to host: transcript | intent | mode | tier | e2e ms | RSS MB.
**Manual check:** `make run` + `go run ./cmd/audiobridge` → say "what time is it" → hear answer;
`results/run-*.jsonl` has a line with `e2e_ms`. Commit.

### Task 3.7: LLM chat path
**Files:** `internal/llm/server.go` (spawn/supervise `llama-server`, `--threads N --ctx-size C --parallel 1
--host 127.0.0.1 --port 8081`, health poll, restart on exit, `Stop()` for T3), `internal/llm/client.go`
(POST `/completion` `{prompt, n_predict, stream:true, cache_prompt:true}`; parse SSE `data:` lines →
`content` chunks → `chan string`; ctx cancel closes the HTTP body), `internal/llm/prompts.go`
(system prompt: 1–2 sentences, reply in user's mode, romanized Tanglish for TANGLISH, never claim actions;
chat template per model family). `Warm()` sends system prompt with `n_predict:0`. Client unit test with
`httptest.Server` emitting canned SSE (pure → in PURE). Pipeline: parser nil → LLM.Stream → clause
splitter → TTS → `out`; 3 s timeout → "quick commands only" reply. Manual: "tell me a fun fact about
space" → audio starts before generation ends. Commit.

**Phase 3 exit check:** English command + chat both work live; JSONL has `t_route` cmd/llm.

---

## Phase 4 — Clips, Tanglish, synthetic data, harness (h8–14)

### Task 4.1: MMS Tamil TTS
If `models/vits-mms-tam` exists from Task 1.2, use it. Else `tools/convert_mms_tamil.py`: follow
sherpa-onnx `scripts/vits` MMS export (download `facebook/mms-tts-tam` on host, export ONNX + tokens.txt,
place in `models/vits-mms-tam/`). Add to `download_models.sh`. Test: synth "வணக்கம்" → wav plays. Commit.

### Task 4.2: buildclips + ClipStore
**Files:** `cmd/buildclips/main.go`, `internal/reply/clips.go` + test.
buildclips (in container): for each `reply.AllFragments()` × mode → synth once with the runtime TTS
(EN: Piper, TA: MMS) → `clips/<sha1(mode+text)>.pcm` + `clips/manifest.json {key: file, ms}`; also writes
`models/hotwords.txt` from lexicon Tamil variants. ClipStore (`iface.ClipStore`): loads all PCM at start;
`Render` = concat fragments with 40 ms crossfade; any missing → `false` (caller falls back to live TTS,
logs `clip_miss`). Test with a fake manifest in `testdata/`. Wire into pipeline behind `O8Clips`.
Manual: "set an alarm for 6" — reply audio starts in < 50 ms after NLU. Commit.

### Task 4.3: Tanglish end to end
Pipeline uses `nlu.LangMode` → TANGLISH templates → Tamil clips. Enable hotwords (`Opt.Hotwords`).
Live test the 13 PRD seed phrases yourself (or via 4.4 synth audio). Extend lexicon from failures.
Commit.

### Task 4.4: Synthetic dataset
**Files:** `cmd/synthdata/main.go`, `data/phrases.yaml`.
`phrases.yaml`: per intent, EN and Tanglish phrasing templates with slot placeholders and spelling
variants (e.g. `{naaliki|naalaikku|nalaiku} {kaalai|morning} {num_ta|num_en} {manikku|ku} alarm {vei|set pannu|vechidu}`).
Expand → sample ~300 (balanced by intent × mode, plus 20 chat prompts) → synthesize (EN Piper; Tanglish
MMS from Tamil-script rendering via a small romanized→Tamil table for lexicon words) with speed ∈
[0.9,1.15] and white noise at 30 dB SNR, 300 ms leading + 1 s trailing silence →
`data/recordings/synth/*.wav` + `labels.jsonl` (`{file, speaker:"synth-<voice>", mode, transcript_gold,
intent_gold, slots_gold}`). Fixed RNG seed. Split dev/test 50/50 by template id. Commit.

### Task 4.5: Harness
**Files:** `cmd/harness/main.go`, `internal/pipeline/replay.go`.
`harness -configs config/ablations/*.yaml -data data/recordings/synth/test -out results/`: for each
config, run the real pipeline with `Transport=replay` (real-time paced WAV + trailing silence), collect
JSONL, compute p50/p95 e2e, mean CPU-s/turn, peak container RSS (`memory.peak` reset between configs by
restarting the container — harness runs **inside** the container, one `docker/run.sh` per config, driven
by `make harness`), WER (word-level Levenshtein vs gold), intent acc, slot exact-match, false cut-off
rate → `results/<config>_<dataset>.csv` + regenerate `results/summary.md`. `make harness-smoke` = 10
utterances, default config. Commit.

### ⛔ Task 4.6: HOUR-14 GATE
Run `make harness-smoke` on Tanglish subset. **Pass:** Tanglish intent acc ≥ 70% on synth test and live
seed phrases work. **Fail:** freeze Tanglish scope (no new Tanglish features; only lexicon tweaks), move on.
If pass and > 10 pts below English, and ≥ 3 h buffer → optional Option A spike (IndicConformer) on a branch.

---

## Phase 5 — Baseline, ablations, degradation (h14–18)

### Task 5.1: Baseline config + Whisper path
**Files:** `internal/asr/whisper.go` (sherpa `OfflineRecognizer` Whisper base; buffers whole utterance,
decodes after endpoint), `config/baseline.yaml`: whisper, SilenceMs 800, O3–O8 off, no `cache_prompt`,
TTS after full LLM reply, default llama-server settings. Verify it produces sound. Commit.

### Task 5.2: Ablation configs
`config/ablations/01_baseline.yaml … 10_full.yaml` cumulative per PRD §10.4 using `extends:`; Tanglish
table configs `t1_raw.yaml` (Fuzzy=false, Merge=false, Hotwords=false), `t2_translit.yaml`,
`t3_fuzzy.yaml` (+Fuzzy+Merge), `t4_hotwords.yaml`. `make ablate` runs all → `results/summary.md`. Commit.

### Task 5.3: Degradation controller
**Files:** `internal/degrade/controller.go` (+ pure tier-selection test), `config/tiers/T0..T3.yaml`,
`host/demo_degrade.sh`. Controller polls cgroup limits every 2 s + rolling p95 of 10 turns →
`SelectTier(cores, memBytes, p95) Tier` (pure, table-tested on PRD §7.8 thresholds). On change: restart
llama-server with tier model/ctx (T3: stop it; all non-command input → "quick commands only" clip).
`demo_degrade.sh`: `docker update --cpus 2 --memory 2g` → `1.5g` → `1g --cpus 1` → `768m`, pausing
for a spoken turn each step; status line shows tier. Note: `docker update --memory` below current usage
can OOM — controller must shrink before the limit drops (demo script waits 3 s after announcing).
Commit.

### Task 5.4 (cut-able): O5 speculative prefill
On stable partial (unchanged for 300 ms) when parser returns nil: send `/completion` with `n_predict:0`
and the partial prompt to warm KV; on endpoint, if final transcript has the partial as prefix, the real
request reuses cache; else nothing to roll back (cache just gets overwritten). Flag `O5SpecPrefill`.

---

## Phase 6 — Numbers & write-up (h18–21)

### Task 6.1: Real recordings
`cmd/record` (host): reads `data/real_prompts.txt` (25 prompts covering all intents, EN + Tanglish),
shows each, records on Enter → `data/recordings/real/<speaker>_<n>.wav` + label line. Ask the user to
get 2–3 people to read (~10 min). Run harness on `real/` for default + baseline + Tanglish table rows.

### Task 6.2: Energy
`host/energy.sh <label> <cmd>`: `sudo powermetrics --samplers cpu_power -i 200 -o results/power_<label>.txt`
around the harness run; 30 s idle baseline first; integrate (W − idle W) × dt → J; divide by turns →
`results/energy.csv`. Needs the user's sudo — ask once, run baseline + full + T1 + T3. Report mean ± std
over 3 runs.

### Task 6.3: Write-up
Update `docs/TECHNICAL_EXPLAINER.md` with measured numbers (replace target-only figures), add
`docs/RESULTS.md`: limits proof (`make limits` output, `docker inspect`), ablation table, Tanglish table
(synthetic vs real separately), RAM/CPU/energy, degradation demo, honest limitations. Optional
`tools/plot.py` charts.

## Phase 7 — Rehearsal (h21–23)
Wi-Fi off. `make run` + bridge. Run the demo script 3×: 5 EN commands, 5 Tanglish commands, 2 chats,
degradation steps, `make limits`. Fix only blocking bugs. Keep push-to-talk ready for a noisy hall.

---

## Verification checklist (before claiming done)
- [ ] `make test` green; `go vet` clean
- [ ] `make gate` prints `GATE PASS`; `make limits` shows 2 CPUs / 2 GiB / only `lo`
- [ ] No `Metal`, `CoreML`, `cuda` in build flags or code (`grep -ri "coreml\|metal" internal cmd docker`)
- [ ] Runtime has no outbound network code except loopback llama-server (`grep -rn "http.Get\|net.Dial" internal cmd`)
- [ ] `results/summary.md` contains baseline and full system rows on synth and real data
- [ ] p50 e2e: command < 0.5 s, chat < 1.0 s (or honestly reported if missed)
- [ ] T3 answers every command intent with clips only
