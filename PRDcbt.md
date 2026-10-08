# PRD — EDGEVOICE: On-Device Tanglish Voice Assistant (HackNex 2026 · HNX26EPS08)

> **For Claude Code:** This is the single source of truth for the project. Read it fully before writing code.
> Sections marked **[DECIDED]** are fixed. Sections marked **[OPEN]** list options with pros/cons and a
> **default** — implement the default unless the team says otherwise, and keep the alternative swappable
> behind the interface described in §6. Never violate anything in §2 (hard constraints) — those are
> pass/fail gates for the whole entry.

---

## 0. TL;DR

A fully offline, CPU-only, voice-in → voice-out assistant that runs inside an enforced resource limit
(target: **2 CPU cores, 2 GB RAM**) on a MacBook Pro M1 Pro. It understands **English and Tanglish**
(Tamil–English code-mixed speech, e.g. *"naliki morning six ku alarm set pannu"*) and replies in the
same register the user spoke in.

Two execution paths:

1. **Command path (fast, deterministic):** ASR → normalize → rule-based intent/slot parser → action →
   templated reply → **pre-synthesized audio clips stitched together**. No LLM. Target < 0.5 s.
2. **Chat path (fallback):** ASR → small quantized LLM (streamed) → clause-level streaming TTS.
   Target < 1.0 s to first audio.

The competitive edge is **Tanglish command understanding on 2 CPU cores**, backed by a measured
ablation. The hackathon is a 24 h event; ~23 h remained at the time of writing.

**Implementation language [DECIDED]:** the **runtime is Go** (orchestration, audio, NLU, metrics,
degradation, harness). Heavy inference stays in C++: speech via **sherpa-onnx Go bindings**, LLM via a
**`llama-server` subprocess** (llama.cpp, CPU-only). **Python is used only for offline tooling** (model
download/conversion, optional plots) and never in the runtime path — except the single fallback noted
in §7.2 (Tamil ASR sidecar) if the chosen Tamil model can't be loaded from Go. One developer builds most
of the runtime, so prefer the simplest Go option every time.

**Hour-1 gate:** confirm the sherpa-onnx Go prebuilt libraries load and run inside the linux/arm64
container. If they don't, fall back to a Python runtime with the same architecture and interfaces.

---

## 1. Problem statement & scoring (verbatim intent)

Build a full ASR → LLM → TTS conversation loop that runs entirely on-device, no cloud, very low compute.
The laptop simulates an edge device: **CPU-only, no GPU, a small slice of cores and RAM enforced via
OS/container limits.** Smaller footprint is better.

**Minimum bar to qualify:**
- Working voice-in → voice-out loop, fully offline, CPU-only.
- Runs under a **declared** resource limit that is **shown enforced**.
- Wake-word / VAD handling.
- **Beats the baseline on end-to-end latency** (end of user speech → first audio of reply) **at a lower
  resource footprint.**
- Report measured **CPU, RAM, and energy**, plus a short write-up.

**Judging rubric:**

| Item | Weight |
|---|---|
| On-device, no cloud, no GPU | **pass/fail gate** |
| End-to-end latency vs baseline | 25% |
| Resource footprint vs baseline (CPU, RAM, energy under declared limit) | 25% |
| Quantization + offload strategy | 15% |
| Graceful degradation when the limit is tightened | 15% |
| Research contribution (what's new vs baseline, shown by ablation/comparison) | 20% |
| Bonus: multilingual, smaller-device operation, cached TTS | bonus |

At judging, judges speak **unseen requests** (expected: short commands in English or Tanglish) and
measure CPU/RAM/energy on our laptop under the declared limit. All judges speak Tamil.

---

## 2. Hard constraints [DECIDED — never violate]

1. **CPU-only.** No Metal, no Apple Neural Engine, no CoreML execution provider.
   - llama.cpp: build with `-DGGML_METAL=OFF` (and no BLAS backend that routes to GPU).
   - whisper.cpp (if used): `-DGGML_METAL=OFF`, no CoreML.
   - ONNX Runtime: **CPUExecutionProvider only.** Never add `CoreMLExecutionProvider`.
   - Inside the Linux container this is automatic (no Metal there) — this is one reason we run in a container.
   - Note for write-up: ARM NEON / Accelerate-AMX instructions are CPU-side; state this explicitly.
2. **Fully offline at inference time.** No network calls in the runtime path. All models are local files.
   Demo with Wi-Fi off.
3. **Enforced resource limit**, declared up front and visible to judges (see §8).
4. **Wake word and/or VAD** must gate the pipeline.
5. **Latency metric definition** (use this everywhere, never a softer one):
   `t_first_audio_out − t_last_voiced_frame`, where `t_last_voiced_frame` is the end of the user's
   actual speech per VAD (NOT the moment the endpointer fires). This honestly includes endpointing wait.

---

## 3. Goals, targets, non-goals

### Targets (under 2 cores / 2 GB)

| Metric | Baseline (expected) | Target |
|---|---|---|
| E2E latency, English chat, p50 | ~2–4 s | **< 1.0 s** |
| E2E latency, command path (EN + Tanglish), p50 | n/a (baseline uses LLM) | **< 0.5 s** |
| E2E latency p95 | — | < 1.5× p50 |
| Peak RSS (all processes in container) | — | **< 2.0 GB** (stretch < 1.5 GB) |
| Energy per turn (J) | measured | lower than baseline |
| Tanglish intent accuracy (held-out recordings) | raw ASR + parser | **≥ 80%** (stretch 90%) |

### Non-goals (do not build)
- Code-switching *within* free-form chat beyond best effort. Only commands are guaranteed.
- Languages other than English and Tamil/Tanglish.
- Real internet-backed intents (weather, news). Answer these with an honest cached "I'm offline" reply.
- GUI polish. A minimal terminal/web status view showing transcript + reply + live metrics is enough.
- Fine-tuning models during the event.

---

## 4. Architecture

```
 HOST (macOS)                         │  CONTAINER (Linux, --cpus=2 --memory=2g)
                                      │
 mic ──► audiobridge (Go) ──sock─┼─► [VAD + endpointer] ──► [ASR router]
 speaker ◄── audiobridge ◄──────sock─┼──                         │
                                      │          ┌────────────────┴───────────────┐
                                      │   English streaming ASR          Tamil ASR (lazy)
                                      │          └────────────────┬───────────────┘
                                      │                   [normalizer: translit + fuzzy lexicon]
                                      │                           │
                                      │                     [language mode: EN | TANGLISH]
                                      │                           │
                                      │                     [intent/slot parser]
                                      │                  match? ──┴── no match?
                                      │                    │              │
                                      │            [action + template]  [LLM, streamed]
                                      │                    │              │ clause chunks
                                      │            [clip stitcher]      [streaming TTS]
                                      │                    └──────┬───────┘
                                      │                     [audio out queue] ──TCP──► host speaker
```

Why a host audio bridge: Docker/Colima VMs on macOS have no direct mic/speaker access. The bridge is a
tiny host process (raw 16 kHz mono int16 PCM over TCP, both directions). Its footprint is measured and
reported separately and honestly.

**Process model inside the container:**
- `edgevoice` — single Go binary: audio I/O over TCP, VAD, ASR, NLU, actions, replies, TTS, metrics,
  degradation controller. Goroutines + channels per stage.
- `llama-server` — llama.cpp's C++ HTTP server, started and supervised by `edgevoice` as a subprocess
  on `127.0.0.1` (loopback only; still fully offline). Restarted with a different model/context when the
  degradation tier changes.
- *(Fallback only)* `tamil_asr_sidecar` — tiny Python process, only if D1=A and the Tamil model can't run
  through sherpa-onnx Go (§7.2).

**Host:** `audiobridge` — small Go binary using `github.com/gen2brain/malgo` (miniaudio, CoreAudio).

### Pipeline principles
- **Everything streams.** No stage waits for the previous stage to fully finish when it can start on partial data.
- **Overlap stages** (see §7.3): speculative prefill, clause-level TTS, lazy model loading triggered early.
- **Every stage emits timestamped events** to a metrics bus (§9). No stage is untimed.
- **Every component sits behind an interface** (§6) so the ablation harness can swap implementations via config.

---

## 5. Repository layout [DECIDED]

```
edgevoice/
  PRD.md                       # this file
  CLAUDE.md                    # short pointer: "read PRD.md first"
  go.mod                       # module edgevoice; Go >= 1.22
  config/
    default.yaml               # chosen defaults
    baseline.yaml              # the baseline config (§10.1)
    ablations/*.yaml           # one file per ablation row
    tiers/*.yaml               # degradation tiers T0..T3
  cmd/
    edgevoice/main.go          # runtime binary (runs in container)
    audiobridge/main.go        # host mic/speaker <-> TCP (malgo)
    harness/main.go            # replay recordings through configs, emit tables (§10)
    buildclips/main.go         # pre-synthesize all template fragments with the runtime TTS (§7.5)
  internal/
    iface/      iface.go       # interfaces from §6
    config/     config.go
    metrics/    bus.go  rss.go  cgroup.go
    audio/      vad.go  endpointer.go  wakeword.go  pcm.go  tcpio.go
    asr/        english.go  tamil.go  router.go  lid.go
    nlu/        translit.go  normalize.go  lexicon.go  intents.go  slots.go  langmode.go
                lexicon.yaml  (embedded via go:embed)
    actions/    alarm.go  timer.go  clock.go  calc.go  system.go
    reply/      templates.go  templates.yaml  clips.go
    llm/        server.go  (spawn/supervise llama-server)  client.go  (SSE streaming)  prompts.go
    tts/        tts.go  clause.go
    degrade/    controller.go
    pipeline/   pipeline.go    # wires stages with channels
  docker/
    Dockerfile                 # CPU-only builds: llama.cpp (llama-server), Go toolchain, sherpa-onnx libs
    run.sh                     # docker run --cpus=X --memory=Y --memory-swap=Y ...
  host/
    energy.sh                  # powermetrics wrapper (sudo), idle subtraction
  tools/                       # Python, OFFLINE ONLY — never imported by the runtime
    download_models.sh
    convert_*.py               # only if a model needs ONNX/GGUF conversion
    plot.py                    # optional charts from results/*.csv
    tamil_asr_sidecar.py       # fallback only (§7.2)
  clips/                       # generated audio fragments + manifest.json
  data/
    recordings/{dev,test}/*.wav + labels.jsonl   # team-recorded commands
  results/                     # harness output CSV/markdown
```

Tests live next to code as `*_test.go` (table-driven). The pure-logic packages (`nlu`, `reply`,
`actions`) must have no cgo/model dependencies so they build and test anywhere in seconds.

---

## 6. Interfaces [DECIDED]

Define these in `internal/iface/iface.go`. Implementations are selected by config.

```go
type PCM []int16 // 16 kHz mono

type VADEvent struct{ Kind int /* SpeechStart|SpeechEnd */; At time.Duration }
type VAD interface{ Process(frame PCM) (*VADEvent, error) }

type Endpointer interface {
    // returns true when the user's turn has ended
    Update(ev *VADEvent, partial string, now time.Duration) bool
}

type Transcript struct{ Text, Lang string; Confidence float32; FinalAt time.Duration }
type StreamingASR interface {
    Lang() string
    Accept(pcm PCM)
    Partial() string
    Finalize() Transcript
    Reset()
}

type NormalizedText struct {
    Tokens       []string
    Canonical    string
    TamilHits    int
    EnglishHits  int
}
type Normalizer interface{ Normalize(t Transcript) NormalizedText }

type Intent struct{ Name string; Slots map[string]string; Score float32 }
type IntentParser interface{ Parse(n NormalizedText) *Intent } // nil => LLM path

type Message struct{ Role, Content string }
type LLM interface {
    Stream(ctx context.Context, msgs []Message, maxTokens int) (<-chan string, error) // token chunks
    Warm(ctx context.Context) error                                                // cache system prompt
}

type TTS interface {
    Lang() string
    Synth(text string) (PCM, error)
}

type ClipStore interface {
    Render(templateID string, slots map[string]string, mode string) (PCM, bool) // false => missing clip
}
```

All timestamps are monotonic offsets from process start (`time.Since(start)`), never wall clock.
Stages are goroutines connected by buffered channels; cancellation via `context.Context` (e.g. barge-in,
speculative-prefill rollback).

---

## 7. Component specs

### 7.1 Audio front end

- **VAD** [DECIDED]: Silero VAD via sherpa-onnx Go (`VoiceActivityDetector`), CPU. 30 ms frames at 16 kHz.
- **Wake word** [OPEN — D15]: see §12. Default: VAD-always-on + optional sherpa-onnx keyword spotter
  (open-vocabulary keywords, no training needed) behind a config flag.
- **Endpointer** [DECIDED approach, OPEN tuning — D12]:
  - Base rule: silence ≥ `silence_ms` (default 400 ms; baseline uses 800 ms).
  - **Semantic early-end**: if partial transcript parses as a *complete command* (intent parser returns
    an intent with all required slots) OR ends with a strong completion cue, end the turn at
    `early_silence_ms` (default 200 ms).
  - Hard cap: max utterance 10 s.
  - Log false cut-offs (user resumed speaking within 600 ms after endpoint) as a metric.

### 7.2 ASR

- **English** [OPEN — D6, default sherpa-onnx streaming zipformer, int8, via Go `OnlineRecognizer`]:
  true streaming, emits partials, supports hotwords (modified beam search).
- **Tamil** [OPEN — D1]: IndicConformer Tamil (ONNX/int8) loaded lazily. Loading path, in order of preference:
  1. through sherpa-onnx Go if a compatible export exists (NeMo CTC/transducer models are supported);
  2. otherwise `tools/tamil_asr_sidecar.py` (onnxruntime CPU) on a Unix socket inside the container,
     started lazily on first Tamil turn. Its RSS is counted in the container total. This is the **only**
     permitted Python in the runtime, and only if D1=A wins the bake-off.
- **Router** [OPEN — D3]: decides which ASR finalizes the utterance.
  - Default: run English streaming ASR always (cheap); run a binary EN/TA language-ID on the first ~1 s
    of voiced audio; if TA, feed buffered audio + remainder into Tamil ASR. **Trigger the Tamil model load
    the moment LID says TA** (load overlaps with the user still speaking).
- Output: `Transcript(text, lang, confidence, t_final)`.

### 7.3 Overlap optimizations (each is an ablation row)

| ID | Optimization | Description |
|---|---|---|
| O1 | Streaming ASR | partial transcripts during speech; near-zero finalize cost |
| O2 | Tuned endpointing | 800 → 400 ms silence |
| O3 | Semantic endpointing | early-end on complete command parse (§7.1) |
| O4 | Prompt KV cache | system prompt prefilled once at startup, reused every turn |
| O5 | Speculative prefill | start LLM prefill on stable partial transcript before endpoint; discard on mismatch |
| O6 | Clause-level TTS | synthesize first clause (split on `, . ? !` or ~8 words) while LLM continues |
| O7 | Command fast path | intent hit → template + clips, LLM skipped entirely |
| O8 | Cached clips | pre-synthesized audio fragments stitched at runtime |
| O9 | LID-triggered lazy load | Tamil models load during speech, not after |

### 7.4 Tanglish NLU (the edge) [DECIDED approach]

**Insight:** in Tanglish commands, English words carry the *content* (alarm, timer, morning, six) and Tamil
words carry the *structure* (when: *naalaikku*; at: *ku/manikku*; do: *pannu/vei*). Both vocabularies are
small and closed → rules + fuzzy lexicon beat a small LLM here on accuracy, latency and RAM.

**Normalizer pipeline** (`internal/nlu/normalize.go`, pure Go, no cgo):
1. If transcript contains Tamil script (Unicode block U+0B80–U+0BFF) → transliterate to Latin in
   `translit.go` with a hand-written table (consonants, vowel signs, pulli ், aytham; lowercase,
   simple ITRANS-like output, e.g. வெதர் → `vedhar`). No ML transliteration model.
2. Lowercase, strip punctuation, split digits from suffixes (`6ku` → `6 ku`).
3. For each token: exact lexicon lookup → else fuzzy match (Levenshtein ≤ 2 on tokens ≥ 4 chars, ≤ 1 on
   shorter; implement Levenshtein inline, ~20 lines; tie-break with a simple phonetic key: collapse
   doubled letters, map dh/th→t, v/w→v, zh/l→l, drop vowels after first char) → else keep raw.
   Transliterated English loanwords (`vedhar`, `alaaram`) must fuzzy-match back to `weather`, `alarm`
   via a `loanwords` section in `lexicon.yaml` that lists their common romanized-Tamil spellings.
4. Convert English and Tamil number words to digits (`six`, `aaru` → `6`).
5. Output `NormalizedText(tokens, canonical_string, tamil_hits: int, english_hits: int)`.

**Lexicon seed** (`internal/nlu/lexicon.yaml`, embedded with `go:embed`; extend from recordings):

| Variants (heard) | Canonical | Role |
|---|---|---|
| naliki, naalaikku, nalaiku, naalaiki, naalaikki | `naalaikku` | DAY=tomorrow |
| innaiku, innaikku, indru, inniki | `innaikku` | DAY=today |
| kaalai, kaalaila, kalaila, morning | `kaalai` | PERIOD=am |
| madhiyam, mathiyam, afternoon | `madhiyam` | PERIOD=pm(12–16) |
| saayangaalam, sayangalam, evening, maalai | `maalai` | PERIOD=pm |
| raathiri, rathiri, night | `raathiri` | PERIOD=pm(night) |
| manikku, manikki, mani, ku, kku, o'clock, oclock | `manikku` | AT |
| nimisham, nimishathula, minute(s), min | `nimisham` | UNIT=minute |
| mani neram, hour(s) | `mani_neram` | UNIT=hour |
| pannu, pannunga, vei, vechidu, vachidu, set, podu | `pannu` | DO |
| cancel, niruthu, stop, venam, vendam | `cancel` | NEG/STOP |
| enna, yenna, what | `enna` | WH |
| time, neram, mani (in "mani enna") | `time` | TIME |
| alarm, alaaram, alarum | `alarm` | INTENT-KW |
| timer, taimer | `timer` | INTENT-KW |
| remind, reminder, nyabagapadutthu, nyabagam | `remind` | INTENT-KW |
| onnu, rendu, moonu, naalu, anju, aaru, ezhu, ettu, onbadhu, pathu, padhinonnu, pannendu | 1–12 | NUM |
| arai, half | `+30min` | NUM-MOD |
| kaal, quarter | `+15min` | NUM-MOD |

**Intents** (`internal/nlu/intents.go`) — keep the set tight:

| Intent | Required slots | Example (EN) | Example (Tanglish) |
|---|---|---|---|
| `alarm.set` | time; day (default: next occurrence) | "set an alarm for 6 o'clock" | "naliki morning six ku alarm set pannu" |
| `alarm.cancel` | — | "cancel my alarm" | "alarm cancel pannu" |
| `timer.set` | duration | "set a timer for 5 minutes" | "anju nimisham timer vei" |
| `reminder.set` | time, text | "remind me to call amma at 7" | "ezhu manikku amma ku call panna nyabagapadutthu" |
| `clock.time` | — | "what time is it" | "time enna" / "mani enna" |
| `clock.date` | — | "what's the date today" | "innaikku date enna" |
| `calc` | expression | "what is 12 times 8" | "12 into 8 evlo" |
| `system.stop` | — | "stop" | "niruthu" / "podhum" |
| `smalltalk.greet` | — | "hi" | "vanakkam" |
| `smalltalk.identity` | — | "who are you" | "nee yaaru" |
| `offline.unsupported` | — | "what's the weather tomorrow" | "naalaikku weather eppadi" |

Parsing approach: keyword/role scoring over canonical tokens + slot regexes over the canonical string
(order-agnostic, since Tamil is SOV and English SVO). Return `None` (→ LLM path) if the top intent's
score < threshold or required slots are missing **and** cannot be defaulted.

**Time slot rules:** hour 1–12 + PERIOD → 24 h. No PERIOD: choose the next future occurrence. `raathiri`
with hour ≤ 4 → AM next day. `+30min` / `+15min` modifiers apply to hour.

**Language mode** (`internal/nlu/langmode.go`) [DECIDED, per team]: if `tamil_hits ≥ 1` → `TANGLISH`, else
`ENGLISH`. Reply language follows the mode. Pure English input → pure English reply.

### 7.5 Replies & clip stitching [DECIDED]

- Templates in `internal/reply/templates.yaml` (embedded), one entry per intent × mode. Each template is a sequence of
  **fragments**; slot values are fragments too.
  - EN: `["Done,", "alarm set for", {hour}, {period_en}, {day_en}]`
  - TANGLISH (screen, romanized): `"Seri, {day} {period} {hour} manikku alarm vechiten"`
  - TANGLISH (TTS, Tamil script): same fragments in Tamil script; English loanwords written in Tamil
    script (e.g. "alarm" → அலாரம்) so a **single Tamil voice** speaks the whole reply with natural Tanglish
    pronunciation (decision D14).
- `cmd/buildclips` pre-synthesizes **every fragment** (template pieces, numbers 1–12, minutes
  5/10/15/20/30/45, day/period words, offline/error replies) for both modes into `clips/*.wav` +
  `clips/manifest.json`, **using the same TTS engine as the runtime** (identical voice). Add ~30–50 ms
  crossfade/silence padding at joins. If D4=C (better Tamil voice for clips), that generator may be a
  `tools/` Python script — output format stays the same.
- At runtime `ClipStore.Render()` loads clips into memory at startup and concatenates them → near-zero TTS latency. If any fragment is missing,
  fall back to live TTS for the whole reply and log it.
- Screen shows romanized Tanglish (or English) transcript + reply.

### 7.6 LLM chat path

- Engine [DECIDED]: **`llama-server`** (llama.cpp, built in the Dockerfile with `-DGGML_METAL=OFF`, no GPU
  backends), spawned by `internal/llm/server.go` with `--threads <cores> --ctx-size <n> --host 127.0.0.1
  --parallel 1`. `client.go` calls `/completion` (or `/v1/chat/completions`) with `stream: true` and
  `cache_prompt: true`, parsing SSE into a token channel. Health-check `/health` before marking ready.
  Changing tier = restart the subprocess with a different GGUF/context.
- Model [OPEN — D5]: default ~1B-class instruct model at **Q4_K_M** GGUF.
- System prompt: short; enforce **1–2 sentence answers**, reply in the user's mode (English or
  romanized Tanglish), never claim to have done actions it didn't do.
- `n_threads` = container cores; context ≤ 1024 tokens; `max_tokens` ≤ 80.
- Prompt KV cache warmed at startup (O4).
- Tanglish chat input representation [OPEN — D13] — default: romanized canonical string.
- Stream tokens → clause splitter → TTS (O6).

### 7.7 TTS

- Engine: sherpa-onnx Go `OfflineTts` (VITS family — runs Piper voices and MMS models in sherpa format).
- English [OPEN — D7]: default Piper (sherpa-onnx VITS export, medium/low voice).
- Tamil [OPEN — D4]: default Meta MMS-TTS Tamil (sherpa-onnx VITS export) for live fallback; cached clips may be generated
  with a heavier/better Tamil TTS **offline on the same laptop before judging** (still fully on-device).
- Output 16 kHz mono int16 PCM; resample in one place only (`internal/audio/pcm.go`, simple linear
  resampler is fine for speech).

### 7.8 Graceful degradation controller

`internal/degrade/controller.go` reads cgroup v2 limits at startup (`/sys/fs/cgroup/cpu.max`, `memory.max`) and
monitors RSS + per-turn latency; selects a tier:

| Tier | Trigger (examples) | LLM | Context | Tamil ASR | TTS |
|---|---|---|---|---|---|
| T0 | ≥ 2 cores, ≥ 2 GB | default (~1–1.7B Q4) | 1024 | lazy | live + clips |
| T1 | < 2 GB or p95 > 1.5 s | ~0.5–0.6B Q4 | 512 | lazy | live + clips |
| T2 | < 1.2 GB or 1 core | ~0.5B Q4 | 256 | load-on-demand, unload after turn | low-quality voice + clips |
| T3 | < 800 MB | **none** | — | commands only | **clips only** |

T3 must still answer every command intent and say a cached "I can only do quick commands right now"
for anything else. Demo plan: tighten the container live and show tiers switching with metrics.

---

## 8. Resource enforcement & measurement [OPEN mechanism — D9; DECIDED requirements]

- Run the pipeline in a Linux container with explicit limits, e.g.
  `docker run --cpus=2 --memory=2g --memory-swap=2g ...` (no swap, so RAM limit is real).
- Show judges: `docker inspect` limits + `cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max` from inside.
- **RAM:** container total from cgroup v2 `memory.current` sampled every 50 ms and `memory.peak`, plus
  per-process `VmRSS`/`VmHWM` from `/proc/<pid>/status` for `edgevoice`, `llama-server` (and sidecar if
  any) — all read directly in Go (`internal/metrics/rss.go`, `cgroup.go`). Report host `audiobridge`
  RSS separately.
- **CPU:** cgroup `cpu.stat` usage_usec per turn → average cores used.
- **Energy:** `sudo powermetrics --samplers cpu_power -i 200` on the host during each run; integrate
  watts × time; **subtract idle baseline** measured with the container idle for 30 s. Report J/turn.
- **Offline proof:** Wi-Fi off during demo; additionally grep/strace-free claim: no network libs used at
  runtime (document it).

---

## 9. Metrics bus

`internal/metrics/bus.go` records per-turn events (monotonic offsets):

`t_speech_start, t_last_voiced, t_endpoint, t_lid, t_asr_final, t_nlu_done, t_route (cmd|llm),
t_llm_first_token, t_first_clause, t_tts_first_chunk, t_first_audio_out, t_turn_done`

Derived: `e2e = t_first_audio_out − t_last_voiced`, plus every stage delta. Also per turn: transcript,
normalized text, intent, slots, mode, tier, peak RSS, CPU-seconds. Write JSONL to `results/`.

---

## 10. Baseline, ablation & evaluation

### 10.1 Baseline [DECIDED — must be credible, not a strawman]
Same container limits. **Whisper base (non-streaming; via sherpa-onnx Go `OfflineRecognizer` Whisper
support, or whisper.cpp's `whisper-server` subprocess if simpler)** → same LLM family at
the *default* llama.cpp settings, no prompt cache → **full reply generated, then** Piper TTS of the whole
reply. Endpointing: 800 ms silence. No command fast path, no clips. (`config/baseline.yaml`)

### 10.2 Test data
- Team records commands: target **≥ 100 utterances**, ≥ 5 speakers, mix of English and Tanglish across all
  intents, varied phrasing and speed. 16 kHz mono WAV.
- `labels.jsonl`: `{file, speaker, mode, transcript_gold, intent_gold, slots_gold}`.
- Split by **speaker**: dev (tune lexicon/thresholds) vs test (never tuned on). Report test only.
- Plus ~20 open chat prompts (EN + Tanglish) for the LLM path.

### 10.3 Harness (`cmd/harness`, Go)
Replays recordings (real-time pacing, with trailing silence) through any config, collects §9 metrics,
outputs per-config: e2e p50/p95, peak RSS, J/turn, CPU-s/turn, WER (gold transcript), intent accuracy,
slot exact-match, false cut-off rate. Writes `results/<config>.csv` and a combined markdown table.

### 10.4 Ablation rows (cumulative, then leave-one-out if time)
1. Baseline
2. + O1 streaming ASR
3. + O2 tuned endpointing
4. + O4 prompt cache
5. + O6 clause TTS
6. + O5 speculative prefill
7. + O7 command fast path
8. + O8 cached clips
9. + O3 semantic endpointing
10. Full system

**Tanglish table (headline research contribution):**
- raw ASR → parser
- + transliteration normalization
- + fuzzy lexicon
- + ASR hotword biasing (if D1 option supports it)
- per row: intent acc, slot exact-match, WER on English-loanword tokens only, e2e latency

**Secondary studies (if time):** LLM input script for Tanglish chat (D13: Tamil script vs romanized vs
mixed) — tokens per utterance + answer quality; quantization sweep (Q3/Q4/Q5/Q8 × model size) →
RAM vs quality Pareto; threads 1/2 vs J/turn (race-to-idle).

---

## 11. Milestones (hours from event start; ~23 h remained at writing)

| By hour | Deliverable | Gate |
|---|---|---|
| 2 | Container with limits running; audio bridge loopback works; models downloaded; baseline config produces sound | **Must pass** |
| 2 | ~50 recordings captured; ASR bake-off (D1) decided | |
| 8 | English streaming loop end-to-end; harness running on recordings; normalizer + parser passing unit tests | |
| 12 | Command fast path + clips integrated (EN + Tanglish) | |
| **14** | **Tanglish end-to-end working — otherwise freeze Tanglish scope** | hard cutoff |
| 18 | All ablation rows run; degradation tiers demonstrable | |
| 21 | Energy/RAM numbers final; write-up done | |
| 23 | Demo rehearsed; buffer | |

---

## 12. Open decisions (pros / cons / default)

> Implement the **default**. Keep alternatives swappable via config where cheap. Update this section when
> the team decides.

### D1 — Tamil/Tanglish ASR
| Option | Pros | Cons |
|---|---|---|
| **A. IndicConformer Tamil (ONNX int8), lazy-loaded** — *default* | Trained on Tamil; handles Tamil words properly; ~100–150 MB | Writes English loanwords in Tamil script (needs translit back); ONNX export/runtime setup can eat hours; may not support hotwords |
| B. English streaming ASR only, parse its output | Zero extra model/RAM; already streaming; English content words survive well | Tamil function words come out garbled (relies heavily on fuzzy lexicon); fails on Tamil-heavy sentences |
| C. Whisper multilingual small (whisper.cpp) | One model for both; decent Tamil | Not streaming → adds post-speech latency; small sizes weak on Tamil; ~250–500 MB |

**Decide by the hour-2 bake-off on real recordings:** pick whichever yields higher *intent accuracy after
normalization*, not raw WER. If B is within ~10 points of A, prefer B (footprint + simplicity).

### D2 — Dedicated Tamil LLM?
| Option | Pros | Cons |
|---|---|---|
| **A. No Tamil LLM. Commands via rules; Tanglish chat via main LLM with romanized input + "reply in Tanglish"** — *default* | No extra RAM; commands (the judged use case) don't need an LLM at all; simplest | Tanglish chit-chat quality mediocre; generic tokenizer is token-inefficient on Tamil (slower) |
| B. Sarvam-1 (2B, Indic tokenizer) lazy-loaded for Tanglish chat | Much better Tamil token efficiency; better Tamil fluency; strong research comparison (tokenizer fertility) | +~1.3 GB at Q4 (breaks 2 GB target unless main model unloads); originally a *base* model — needs an instruct variant/workaround; GGUF availability uncertain |
| C. Community Indic Qwen-1.5B fine-tune | Smaller than Sarvam; instruct-tuned | Unverified quality; still generic tokenizer issues |

**Default A.** Only try B after hour 14 and only if a chat-capable GGUF exists and RAM allows (e.g. as
the *only* LLM in a Tanglish-heavy config). Reporting A vs B tokens-per-sentence is a cheap research
add-on even if B isn't used in the demo.

### D3 — Language identification
| Option | Pros | Cons |
|---|---|---|
| **A. Binary acoustic LID (e.g. VoxLingua107 ECAPA ~20 MB) on first ~1 s** — *default* | Decides early → enables O9 lazy-load overlap; robust to ASR errors | Extra model; Tanglish starting with English words may be misrouted |
| B. Lexicon-based after English ASR (tamil_hits) | No extra model | Decides late (after speech) → no load overlap; depends on garbled English-ASR output |
| C. Run both ASRs, pick by confidence | Most accurate | Doubles ASR CPU on 2 cores → hurts footprint score |

Note: reply-language **mode** (§7.4) is always lexicon-based; LID only routes the ASR. If D1 = B, LID is
unnecessary.

### D4 — Tamil TTS
| Option | Pros | Cons |
|---|---|---|
| **A. MMS-TTS Tamil (VITS) live + clips** — *default* | Small, fast on CPU, single voice | Robotic quality |
| B. AI4Bharat Indic-TTS | Better quality | Heavier, slower, more setup |
| C. Clips pre-generated with the best available Tamil TTS before judging; MMS only for live fallback | Command replies sound best; still on-device | Mixed voice quality between clip and live replies |

Default A, upgrade clips to C if time permits.

### D5 — Main LLM size/model
| Option | Pros | Cons |
|---|---|---|
| **A. ~1B instruct (e.g. Llama 3.2 1B / Gemma 3 1B / Qwen3 ~0.6–1.7B), Q4_K_M** — *default* | Balanced quality/speed; ~0.7–1.1 GB | Weak facts |
| B. ~0.5–0.6B | Fastest, smallest; frees RAM for Tamil models | Noticeably dumber |
| C. ~1.7B | Better answers | ~1.2 GB+; slower decode on 2 cores |

Pick by a 15-minute benchmark (TTFT + tok/s on 2 cores + 10 prompt sanity check). Smaller tiers come from
the same family for §7.8.

### D6 — English ASR
| Option | Pros | Cons |
|---|---|---|
| **A. sherpa-onnx streaming zipformer (int8)** — *default* | True streaming partials; small; supports hotwords | English-only model choice limited |
| B. Moonshine | Edge-optimized; latency scales with audio length | Not truly streaming partials |
| C. whisper.cpp tiny/base | Familiar | Non-streaming — this is the baseline's weakness |

### D7 — English TTS
| Option | Pros | Cons |
|---|---|---|
| **A. Piper** — *default* | Very fast on CPU, tiny | Less natural |
| B. Kokoro-82M | More natural | Heavier, slower first chunk |
| C. KittenTTS | Tiny | Quality/maturity uncertain |

### D8 — Orchestrator language
| Option | Pros | Cons |
|---|---|---|
| A. Python (asyncio) — *superseded* | All model libs native; fastest to build | ~100–300 MB interpreter overhead; GIL (mitigated: heavy work is in C++ libs that release it) |
| **B. Go orchestrator + model servers (llama-server, sherpa via cgo/subprocess)** — *DECIDED (see §0)* | Lower RAM; clean concurrency | cgo/bindings risk under a 24 h clock |

**DECIDED: B (Go).** Python runtime only as the hour-1-gate fallback (§0).

### D9 — Enforcement mechanism
| Option | Pros | Cons |
|---|---|---|
| **A. Docker Desktop `--cpus --memory --memory-swap`** — *default* | Visible, standard, judges recognize it; live re-limit via `docker update` for degradation demo | Desktop VM overhead; audio needs bridge |
| B. Colima/Lima VM with fixed vCPU/RAM | Lighter than Docker Desktop | Changing limits needs VM restart (bad for live degradation demo) |
| C. Native macOS + thread caps | Simplest audio | **Not enforced** — fails the "shown enforced" requirement |

### D10 — Declared limit
| Option | Pros | Cons |
|---|---|---|
| **A. 2 cores / 2 GB** — *default* | Comfortable headroom for full stack | Others may declare smaller |
| B. 2 cores / 1.5 GB | Better footprint story | Little room for Tamil models + 1B LLM together |
| C. 1 core / 1 GB | Most impressive | LLM path slow; likely forces T2/T3 |

Declare A; demonstrate B and C live as degradation tiers.

### D11 — Speculative prefill (O5)
Pros: saves prefill time on long utterances. Cons: complexity; wasted CPU/energy on rollback; small gain
for short commands (which skip the LLM anyway). **Default: implement last, only for the chat path.**

### D12 — Semantic endpointing
| Option | Pros | Cons |
|---|---|---|
| **A. Parser-complete rule (command fully parsed → early end)** — *default* | Free (reuses parser); big win exactly on commands | Only helps commands |
| B. Small turn-detector classifier on partials | Helps chat too | Extra model, training/finding one takes time |

### D13 — Script fed to LLM for Tanglish chat
Options: Tamil script / romanized / mixed. **Default romanized canonical string**, then measure all three
(tokens/utterance + answer quality) as a research study.

### D14 — Speaking Tanglish replies
| Option | Pros | Cons |
|---|---|---|
| **A. Transliterate English words into Tamil script; one Tamil voice** — *default* | Single consistent voice; natural Tanglish accent | Needs a loanword→Tamil-script map for template words |
| B. Split by language, two voices | Correct English pronunciation | Jarring voice switches mid-sentence |

### D15 — Wake word
| Option | Pros | Cons |
|---|---|---|
| **A. VAD-gated always listening + optional sherpa-onnx keyword spotter behind flag** — *default* | Satisfies "wake-word / VAD" requirement; no extra latency | False triggers in noisy hall |
| B. Mandatory wake word | Robust in noise | Extra model; custom word training takes time; adds a step to every demo turn |

Keep a push-to-talk key as an emergency fallback for a noisy judging hall.

---

## 13. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Audio into container fails / glitches | Build the bridge first (hour 0–2); loopback test; fallback: run audio on host, models in container over TCP (already the design) |
| Accidentally using Metal/ANE | CPU-only builds inside Linux container; document build flags in write-up |
| Tamil ASR export pain | Bake-off decides early; option D1-B needs no extra model |
| Judges speak Tanglish phrasing we didn't see | Diverse recordings from all team members; fuzzy matching; graceful "sorry, didn't get that" + LLM fallback |
| Noisy judging hall breaks VAD | Tune VAD threshold on-site; push-to-talk fallback |
| Energy numbers noisy | Multiple runs, idle subtraction, report mean ± std |
| Time overrun | Hard cutoff at hour 14 for Tanglish; ablation harness built early so numbers exist regardless |

---

## 14. Working conventions for Claude Code

- Go ≥ 1.22 runtime; `go test` table-driven tests, `go vet` + `gofmt`. Python ≥ 3.11 only in `tools/` (offline).
- Container runs with `--network=none`; host audiobridge talks over a Unix socket in a bind-mounted dir (see docs/DESIGN.md).
- **Pure-logic modules first** (normalize, lexicon, intents, slots, langmode, templates) — they have no
  model dependencies; write unit tests with both English and Tanglish cases (include messy spellings).
- No component reads config globals; pass config objects in.
- Every new optimization gets a config flag so the harness can toggle it.
- Never add a network call to the runtime path. Never enable GPU/Metal/CoreML providers.
- Model paths come from config; `tools/download_models.sh` is the only place that touches the internet.
- Keep RAM in mind: lazy-load, `mmap` GGUF, unload Tamil models in tiers T2+.
- When unsure between options, follow the **default** in §12 and leave a `// DECISION(Dx)` comment.
- Confirmed overrides and the full design live in `docs/DESIGN.md`; the task plan is in `docs/plans/`.

### Seed test cases (extend from recordings)

| Input | Mode | Intent | Slots |
|---|---|---|---|
| naliki morning six ku alarm set pannu | TANGLISH | alarm.set | day=tomorrow, time=06:00 |
| set an alarm for 6 o'clock | ENGLISH | alarm.set | time=06:00 (next occurrence) |
| naalaikku kaalai aaru manikku alarm vei | TANGLISH | alarm.set | day=tomorrow, time=06:00 |
| raathiri pathu manikku alarm vechidu | TANGLISH | alarm.set | time=22:00 |
| anju nimisham timer vei | TANGLISH | timer.set | duration=5m |
| set a timer for ten minutes | ENGLISH | timer.set | duration=10m |
| time enna | TANGLISH | clock.time | — |
| what time is it | ENGLISH | clock.time | — |
| alarm cancel pannu | TANGLISH | alarm.cancel | — |
| naalaikku weather eppadi irukkum | TANGLISH | offline.unsupported | — |
| vanakkam | TANGLISH | smalltalk.greet | — |
| nee yaaru | TANGLISH | smalltalk.identity | — |
| tell me a fun fact about space | ENGLISH | → LLM path | — |
