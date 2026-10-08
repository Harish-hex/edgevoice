# EdgeVoice — Validated Design

> Output of the brainstorming session on 2026-10-08. `PRDcbt.md` remains the spec; this document records
> the confirmed decisions that override or refine it, and the design that the implementation plan
> (`docs/plans/2026-10-08-edgevoice-implementation.md`) executes; verified reference code for `iface`/`nlu` is in `docs/plans/reference/`. Background/rationale for judges:
> `docs/TECHNICAL_EXPLAINER.md`.

## 1. Understanding summary

- **What:** fully offline, CPU-only voice-in → voice-out assistant for English + Tanglish, running in a
  Docker container limited to 2 cores / 2 GB on an M1 Pro (HackNex 2026, HNX26EPS08).
- **Why:** beat a Whisper → LLM → Piper baseline on e2e latency and footprint; research edge is
  rule-based + fuzzy-lexicon Tanglish command understanding instead of an LLM.
- **Who:** Tamil-speaking judges issuing unseen short commands in English or Tanglish.
- **Paths:** command fast path (streaming ASR → normalizer → parser → clips, < 0.5 s) and chat path
  (llama-server streaming → clause TTS, < 1.0 s to first audio).
- **Hard gates:** no GPU/Metal/ANE/CoreML; offline; visibly enforced limits; VAD gating; latency =
  `t_first_audio_out − t_last_voiced_frame`.
- **Non-goals:** GUI polish, internet intents, fine-tuning, other languages, guaranteed code-mixed chat.

## 2. Assumptions

1. Solo developer, ~23 h. Sequential plan with a cut list and a hard Tanglish freeze at hour 14.
2. Docker Desktop is installed (daemon currently not running; Claude starts it).
3. Teammates/friends can give ~10 minutes to read 20–30 prompts for the real test set.
4. sherpa-onnx Go prebuilt libs work on linux/arm64 — verified by the hour-1 gate; else Python fallback.
5. All PRD §12 decisions not listed below use their PRD default.

## 3. Decision log

| # | Decision | Alternatives considered | Why |
|---|---|---|---|
| 1 | **Go runtime** (PRD §0); D8/§14 Python text fixed in PRD | Python asyncio | ~100–300 MB less RAM under a 2 GB cap; goroutines/channels fit streaming; single binary |
| 2 | **D1 = B first**: English streaming zipformer + translit + fuzzy lexicon; no LID (D3 moot) | A: IndicConformer + LID + lazy load; C: Whisper multilingual | No extra model/RAM, no Python sidecar, lowest solo-dev risk. A is an upgrade after hour 14 only if B is > 10 pts worse |
| 3 | **Hybrid test data**: ~300 TTS-synth utterances + 20–30 real recordings (held-out) | Synthetic only; real only (≥100, ≥5 speakers) | Volume for ablation/latency + an honest real-speech accuracy check; reported separately |
| 4 | **Build strategy A**: infra gate by h2 → pure NLU via TDD during waits → thin English loop → Tanglish + clips by h14 | Logic-first; harness-first | Risky infra fails early while fallback is cheap; build/download waits are used |
| 5 | **`--network=none`** + audio bridge over a **Unix socket** in a bind-mounted dir | Bridge network + TCP port | Offline is enforced, not claimed. Fallback: stdin/stdout transport (`docker run -i`), still no network |
| 6 | Pure packages (`nlu`, `reply`, `actions`) tested natively; cgo code built only in the container | Cross-build cgo for macOS too | One cgo target, less debugging |
| 7 | Normalizer also tries **merging adjacent tokens** before fuzzy match | Single-token only | English ASR splits Tamil words ("nali key" → naalaikku) |
| 8 | Harness **replay mode reuses the live pipeline** (WAV reader replaces socket input) | Separate offline path | Harness numbers are the real system's numbers |
| 9 | Wake word: sherpa-onnx keyword spotter behind a flag (PRD D15 said openWakeWord) | openWakeWord | Same library as VAD/ASR; no extra runtime |
| 10 | `t_first_audio_out` = first reply PCM written to the output socket | At host speaker | Host playback buffer is constant across configs; documented |
| 12 | Fuzzy threshold ≤2 edits only for tokens of 6+ runes (≤1 for 3–5) | PRD: ≤2 from 4 runes | PRD rule matched "tell"→"ten"; measured in Tanglish ablation |
| 11 | Claude drives infra/Docker/build debugging autonomously | User debugs | User preference |

## 4. Design

### 4.1 Environment & topology
- One Go module `edgevoice`. Native macOS: `make test` runs pure packages only.
- `docker/Dockerfile` (multi-stage): `llama-build` (llama.cpp `llama-server`, `-DGGML_METAL=OFF
  -DGGML_BLAS=OFF`, native CPU) → `sherpa` (sherpa-onnx Go libs, CPU EP only) → `runtime` (Debian slim +
  Go toolchain). Models are **mounted read-only** at `/models`, never baked into the image.
- `docker/run.sh`: `--cpus=$CPUS --memory=$MEM --memory-swap=$MEM --network=none -v ./sock:/sock
  -v ./models:/models:ro -v .:/src`.
- Host: `cmd/audiobridge` (malgo) connects to `./sock/audio.sock` (container listens). Frames:
  4-byte little-endian length + int16 PCM, 16 kHz mono, both directions; a control byte stream for
  push-to-talk.
- `tools/download_models.sh` is the only internet-touching script; writes `models/manifest.txt` with SHA256.
- `make gate`: inside container, VAD + zipformer on one WAV, Piper synth, llama-server `/health`, print
  `cpu.max`/`memory.max`. Pass → Go runtime confirmed.

### 4.2 Pipeline
```
socketIn → framer(30ms) → VAD → endpointer ─┐
                    └──► ASR.Accept (partials)
endpoint → ASR.Finalize → Normalize → Parse
   ├─ intent → action → template → ClipStore.Render ─┐
   └─ nil    → LLM.Stream → clauseSplitter → TTS ─────┤
                                         audioOut → socketOut
```
- Goroutine per stage, buffered channels, per-turn `context.Context` (cancel on barge-in / stop).
- Endpointer calls the parser on each partial; complete command → end at 200 ms silence (else 400 ms;
  baseline 800 ms; hard cap 10 s). False cut-off = speech resumes ≤ 600 ms after endpoint.
- Metrics bus: `Mark(turn, name)` with offsets from process start; sampler every 50 ms reads
  `memory.current`, `memory.peak`, `cpu.stat`, `/proc/<pid>/status`. JSONL per turn to `results/`.
- Errors degrade, never crash: ASR fail → "didn't catch that" clip; LLM timeout 3 s / crash → "quick
  commands only" clip + supervisor restart; missing clip → live TTS (logged); per-turn panic recovery.
- One config struct passed explicitly; every optimization O1–O9 has a flag.

### 4.3 NLU (`internal/nlu`, pure Go)
- `translit.go` Tamil script → Latin table. `lexicon.yaml` (embedded) → `exact` map + `byPhoneticKey`
  buckets. `normalize.go`: lowercase → split digit suffixes → adjacent-token merge → exact → phonetic
  bucket + Levenshtein (≤2 for len ≥4, ≤1 shorter) → raw. Numbers → digits. Tamil/English hit counts.
- `intents.go` data-driven intents (weighted keywords, required slots, defaults); threshold 0.5 → nil.
- `slots.go` time/day/duration rules from PRD §7.4 with injected `now`.
- `langmode.go` tamil_hits ≥ 1 → TANGLISH.
- Zipformer hotwords from lexicon Tamil keys (flag; Tanglish-table row).

### 4.4 Replies, clips, TTS, LLM
- `templates.yaml`: per intent × mode, `display` (romanized) + `speech` fragments (Tamil script for
  Tanglish with loanwords like அலாரம்; single MMS-Tamil voice).
- `cmd/buildclips`: synthesize each unique fragment once → `clips/<hash>.pcm` + `manifest.json`.
  `ClipStore` loads all at startup, 40 ms crossfade; any missing fragment → live TTS for whole reply.
- TTS: sherpa-onnx `OfflineTts` (Piper EN, MMS TA). Single resample point `audio/pcm.go`.
- LLM: supervisor spawns `llama-server --threads N --ctx-size 1024 --parallel 1 --host 127.0.0.1`,
  polls `/health`, `Warm()` primes system prompt; client streams `/completion` (`cache_prompt`,
  `n_predict: 80`). Clause splitter on `,.?!` or 8 words. Model chosen by `tools/bench_llm.sh` (TTFT,
  tok/s, 10 sanity prompts) among Qwen3-0.6B / Llama-3.2-1B / Gemma-3-1B at Q4_K_M. O5 last.

### 4.5 Degradation, data, harness, verification
- `degrade/controller.go`: polls `cpu.max`/`memory.max` every 2 s + rolling p95 of last 10 turns →
  T0–T3 (PRD §7.8). Tier change restarts/stops llama-server. `make demo-degrade` steps `docker update`.
- `cmd/synthdata`: phrasing templates × spelling variants × slot values → ~300 WAVs (Piper EN, MMS TA,
  speed 0.9–1.15×, light noise) + `labels.jsonl`. `cmd/record` (host): prompt → Enter → WAV + label.
- `cmd/harness`: configs × datasets via replay → p50/p95 e2e, peak RSS, CPU-s, intent acc, slot EM, WER,
  false cut-offs → `results/<config>_<dataset>.csv` + `results/summary.md`; energy joined from
  `host/energy.sh` logs.
- Baseline (`config/baseline.yaml`): sherpa-onnx Whisper-base offline, 800 ms endpoint, no prompt cache,
  full reply then TTS, no fast path, no clips.
- Gates: `make gate` (h2), `make test`, `make harness-smoke`, final Wi-Fi-off demo check.

## 5. Risks (acknowledged)
| Risk | Mitigation |
|---|---|
| sherpa-onnx Go libs fail in container | Hour-1 gate; Python fallback with same interfaces |
| Unix socket across Docker Desktop VM flaky (bind-mounted sockets often don't cross the VM boundary) | Fallback keeps `--network=none`: bridge spawns `docker run -i` and streams frames over the container's stdin/stdout |
| Option B too weak on Tamil-heavy speech | Merge + fuzzy + hotwords; A upgrade after h14 if > 10 pts gap |
| Synthetic data inflates accuracy | Real held-out set reported separately |
| Time overrun | Cut list in plan; hour-14 Tanglish freeze |
