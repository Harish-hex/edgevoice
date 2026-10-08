# EdgeVoice: Technical Pitch Script and Judge Q&A Prep

> **How to use this:** Part 1 is the spoken pitch (~4 minutes). Part 2 is the live demo run-sheet.
> Part 3 is the deep-dive you must *understand*, not memorise: the judges will probe here.
> Part 4 is likely questions with crisp answers. Part 5 is a glossary.
> **Rule:** only say numbers from the "Measured" column. Anything marked ⏳ is not measured yet. Say so
> plainly if asked ("that run is in progress"); never round a target into a result.

---

## Part 1: The pitch (spoken, ~4 min)

**Opening (20 s)**
> "EdgeVoice is a voice assistant that runs entirely on-device: no cloud, no GPU, inside a container
> we've hard-limited to **2 CPU cores and 2 GB of RAM, with networking disabled**. It understands
> **English and Tanglish**, the Tamil–English code-mixing people actually speak, like
> *'naalaikku kaalai aaru manikku alarm vei'*, and it answers in the same register."

**The problem (30 s)**
> "The standard edge pipeline is ASR → LLM → TTS, run one after another. On two CPU cores that's slow,
> because each stage waits for the previous one to finish, and the LLM is the slowest part. It's also
> wasteful: most voice requests are short commands (alarms, timers, time, date) that don't need a
> language model at all. And off-the-shelf English speech models fail completely on Tanglish. We
> measured it: on real Tanglish speech an English recognizer outputs unrelated English words."

**Our approach: two paths (60 s)**
> "So we split the work into two paths.
> The **command fast path**: speech goes through a streaming recognizer, then a **rule-based Tanglish
> parser**, then the action, and the reply is stitched from **pre-synthesized audio clips**. No LLM,
> no live speech synthesis. That answers in about **0.3 s for English and 0.5–0.6 s for Tanglish**,
> measured from the end of your speech to the first audio out.
> The **chat path** handles everything else: a **quantized 0.6-billion-parameter LLM** running on
> llama.cpp, CPU only, streams its answer and we **speak it phrase by phrase** while it's still
> generating."

**The research contribution: Tanglish (60 s)**
> "The key insight for Tanglish commands: **English words carry the content** (alarm, timer, six)
> **and Tamil words carry the structure** (*naalaikku* = tomorrow, *manikku* = at o'clock,
> *vei* = set it). Both vocabularies are small and closed, so rules plus a fuzzy lexicon beat a small
> LLM on accuracy, latency and RAM.
> For recognition we run **two recognizers in parallel**: a streaming English Zipformer, and
> **AI4Bharat's IndicConformer for Tamil**, int8-quantized. We don't pick a language up front. Both
> transcripts go through our parser, and **whichever one parses into a valid command wins**. We call
> that *parse-based arbitration*. It needs no language-ID model, and it handles sentences that start
> in English and finish in Tamil."

**Engineering for the constraint (40 s)**
> "Everything is built for the limit. The runtime is a single **Go** binary: no Python interpreter
> overhead. Heavy inference stays in C++: **sherpa-onnx** for speech, **llama.cpp** for the LLM, and
> both are pinned to the **CPU execution provider**. We read memory and CPU straight from the
> container's **cgroup v2** files, and the assistant **degrades gracefully**: if the limits are
> tightened, it switches to smaller tiers, down to commands only with cached audio."

**Close (20 s)**
> "Everything you'll see on the dashboard is measured live from inside the container: per-stage
> latency, working memory against the 2 GB limit, CPU against 2 cores. Let me show you."

---

## Part 2: Demo run-sheet

1. Before the judges arrive: Wi-Fi **off**, Docker Desktop running, then `make run MIC=0` (earphone mic).
   The dashboard opens at `localhost:8080`.
2. Show enforcement: in a second terminal run `make limits`, which prints `cpu.max: 200000 100000` (2 cores),
   `memory.max: 2147483648` (2 GiB), and only `lo` up (no network).
3. Say "**what time is it**" with no wake phrase. It's *ignored*. Point at "Asleep" on the dashboard.
4. "**Hey Computer**" → it replies "Yes?". Then "**naalaikku enna date**". Point at:
   both recognizers' transcripts and which one won (✓), the intent `clock.date`, `day = tomorrow`, the path
   "rules + cached clips, no LLM", and the latency bar.
5. Follow-up within 8 s: "**innaikku illa, naalaikku**" ("not today, tomorrow") is handled as a correction.
6. "**Hey Computer, naalaikku kaalai aaru manikku alarm vei**" → alarm 06:00 tomorrow.
7. "**Hey Computer, tell me a fun fact about space**" → LLM path. Point at the stage breakdown
   (LLM first token, then speech synthesis) and that speech starts before the LLM finishes.
8. Point at Resources: working memory (~1.1–1.2 GB) against the 2048 MB limit, CPU cores in use.
9. (If degradation is ready) tighten live: `docker update --cpus 1 --memory 768m --memory-swap 768m edgevoice`,
   and the tier badge drops to T3 (commands only).
10. **Backup if the hall mic fails:** `go run ./cmd/audiobridge -feed <wav files>` plays recordings
    into the same pipeline, and the dashboard still updates live.

---

## Part 3: What we built, end to end (know this cold)

### 3.1 Architecture

```
HOST (macOS)                              │  CONTAINER (Linux arm64, --cpus=2 --memory=2g --memory-swap=2g --network=none)
mic ─► audiobridge (Go, miniaudio) ─stdin─┼─► VAD (Silero) ─► endpointer ─┬─► streaming EN ASR (Zipformer)
speaker ◄─ audiobridge ◄────────── stdout─┼──                             └─► (on endpoint) TA ASR (IndicConformer)
dashboard (localhost:8080, SSE) ◄─ events─┼──     both transcripts ─► normalizer ─► parser ─► arbitration
                                          │        ├─ command ─► action ─► template ─► cached clips ─┐
                                          │        └─ no command ─► llama-server (Qwen3-0.6B Q4_K_M) │
                                          │                          ─► phrase splitter ─► Piper TTS ─┤
                                          │                                              audio out ◄──┘
```

- **Why a host bridge?** Docker on macOS runs in a Linux VM with no access to the mic or speaker. The
  bridge is a tiny host process that streams raw **16 kHz mono 16-bit PCM**. The container is started
  with `docker run -i` and audio flows over its **stdin/stdout** as length-prefixed frames, which is how
  we keep `--network=none`. (We first tried a Unix socket in a shared folder, but sockets don't reliably
  cross Docker Desktop's VM boundary.)
- **Process model inside the container:** one Go binary (`edgevoice`) with one goroutine per stage,
  connected by channels, plus `llama-server` as a supervised child process bound to `127.0.0.1`
  (loopback only; still no network interface).

### 3.2 Hard constraints, and how we guarantee each

| Constraint | How it's enforced / proven |
|---|---|
| CPU only | llama.cpp built with `GGML_METAL=OFF GGML_BLAS=OFF GGML_CUDA=OFF GGML_VULKAN=OFF`; ONNX Runtime `provider = "cpu"` hard-coded; it runs in a Linux container, where Metal doesn't exist anyway |
| Offline | `--network=none` (only loopback). `tools/download_models.sh` is the only script that uses the internet; the runtime never does |
| Enforced limit | cgroup v2: `cpu.max` (CPU quota: 200000 µs per 100000 µs period = 2 cores), `memory.max` = 2 GiB, `--memory-swap` equal to memory so there's **no swap** to hide behind |
| Wake/VAD | Silero VAD gates everything; the "Hey Computer" wake phrase gates acting on it |
| Honest latency | `t_first_audio_out − t_last_voiced_frame`: from the end of *actual speech*, so the endpointing wait is included |

Note for the write-up: ARM **NEON** SIMD and fp16/dot-product instructions are **CPU** instructions.
We compile llama.cpp for `armv8.2-a+dotprod+fp16` (we had to set this explicitly because GCC inside
the VM couldn't auto-detect fp16). That's still CPU-only, not GPU or Neural Engine.

### 3.3 Audio front end

- **VAD: Silero** (sherpa-onnx), 512-sample windows = **32 ms** at 16 kHz, speech threshold **0.35**
  (lowered from 0.5 for quiet headset mics), 100 ms *hangover*. We subtract the hangover to recover the
  true end of speech for the latency metric.
- **Pre-roll:** we keep the last **640 ms** of audio before the VAD fires, so the first syllable isn't lost.
- **Endpointer:** the turn ends after **400 ms of silence**. The baseline uses 800 ms. **Semantic
  endpointing:** if the partial transcript *already parses as a complete command*, we end after only
  **200 ms**. That reuses the parser, so it costs microseconds.
- **Half-duplex:** mic input is dropped while the assistant is speaking, so the laptop speaker can't
  trigger the assistant.
- **Wake phrase "Hey Computer"**: detected in the *recognizer transcripts*, not by a separate
  keyword-spotting model. "Computer" is pronounced almost the same by all speakers and is a common
  loanword, so both recognizers handle it. Tamil script கம்ப்யூட்டர் is transliterated back to match.
  You can say it alone (it answers "Yes?" / "Sollunga?") or in the same breath as the command. After
  each reply there's an **8 s follow-up window** where no wake phrase is needed.

### 3.4 Speech recognition: dual ASR with parse-based arbitration

| | English | Tamil |
|---|---|---|
| Model | sherpa-onnx **streaming Zipformer transducer** (2023-06-26), int8 encoder/joiner | **AI4Bharat IndicConformer** (Tamil, ~120M params), **CTC head**, **int8 dynamic quantization** |
| Size on disk | 70 MB | 131 MB |
| Mode | **Streaming**: partial transcripts *while you speak* | **Offline**: one pass after the endpoint |
| Decoding | greedy search over BPE tokens | greedy CTC over a Tamil-only 256-token vocabulary (+ blank) |
| Cost after you stop | ~30–50 ms (finalize) | ~100–190 ms on 2 cores, run in parallel with the English finalize |

- **Transducer (RNN-T) vs CTC:** a transducer has a prediction network (an internal language model) and
  decodes frame by frame, which makes it ideal for streaming. CTC predicts a token or *blank* per frame
  independently and collapses repeats: simpler, and very fast for one-shot decoding.
- **Why not just one Tamil model?** It writes English commands in Tamil script ("செட் அன் அலாரம்") and
  can't stream, so English would get slower and worse.
- **Why not a language-ID model to pick the recognizer?** It's an extra model, and Tanglish often
  *starts* with English words ("alarm cancel pannu"), so it misroutes exactly the hard cases.
- **Arbitration (`nlu.Choose`):** (1) a transcript that parses into a command beats one that doesn't;
  (2) if both parse, take the higher parser score, then higher **lexicon coverage** (fraction of content
  words recognised), then English; (3) if neither parses (chat), use English unless the Tamil
  transcript is clearly Tanglish (coverage ≥ 0.4).
- **Bug we found and fixed (good story):** IndicConformer **drops the first word** when an utterance
  starts with ≥300 ms of silence. We measured this by padding the same clip with 0/100/300/600 ms of
  silence. The likely cause is per-utterance feature normalisation being skewed by silence. Fix: trim
  silence to a 50 ms margin before the Tamil decode, using a loudness-relative threshold (10% of the
  loudest 20 ms frame) so it adapts to mic noise.

### 3.5 Tanglish NLU (pure Go, no model)

Pipeline: **transliterate → tokenize → split glued keywords → lexicon lookup (exact → fuzzy →
phonetic) → merge adjacent tokens → numbers → intent rules → slot extraction**.

- **Transliteration:** a hand-written Tamil-script → Latin table (consonants, vowel signs, pulli ்)
  with context rules: **gemination** (doubled consonants) is voiceless, so ட்ட→`tt` and எட்டு→`ettu`;
  word-initial ட→`t`, so டைமர்→`taimar`; ஞ்ச→`nj`, so அஞ்சு→`anju`.
- **Lexicon:** about 50 canonical entries with **several hundred spelling variants**, each tagged with
  a **role** (DAY, PERIOD, AT, UNIT, DO, NEG, WH, NUM, KW, WAKE, …). Example: *naliki, nalaiku,
  naalaikki, நாளைக்கு* → `naalaikku` = DAY:tomorrow.
- **Fuzzy matching:** **Levenshtein edit distance** ≤2 for words of 6+ letters, ≤1 for 4–5 letters,
  exact only for ≤3 letters. We tightened this from the PRD because "tell" matched "ten". Ties are broken
  by a **phonetic key** (dh/th→t, c/k/q→k, w→v, zh→l, final y→i, doubled letters collapsed, vowels
  dropped after the first letter), so `manicu` ≈ `manikku` and `caulay` ≈ `kaalai`.
- **Token merge / split:** the English recognizer splits Tamil words ("nali key" → `naalaikku`) and glues
  others ("timervey" → `timer vey`). We handle both.
- **Intents** (rule-based, order-agnostic because Tamil is SOV and English SVO): alarm.set,
  alarm.cancel, timer.set, reminder.set, clock.time, clock.date, calc, system.stop, smalltalk.greet,
  smalltalk.identity, offline.unsupported. Score = 1 − 0.15 × (unknown content words); threshold
  0.5. Below that, or with a required slot missing, the request goes to the LLM.
- **Slots:** hour + period → 24 h (*raathiri* + hour ≤ 4 → early morning next day); with no period we
  choose the **next future occurrence**; *arai* = +30 min, *kaal* = +15 min; durations: NUM + UNIT.
- **Language mode:** any Tamil-lexicon hit → TANGLISH, so the reply is in Tanglish; otherwise English.
- **Follow-ups:** a day-only utterance within 30 s of a date question re-asks it for the new day.

### 3.6 Replies: templates + cached clips (no TTS at answer time)

- 15 reply templates × 2 modes. Each template is a list of **fragments**; slot values are fragments too.
- `cmd/buildclips` pre-synthesises **every fragment** (415 clips) with the *same voices the runtime
  uses*, and they're loaded into memory at startup. The reply is the clips **concatenated with a 40 ms
  crossfade**, so stitching takes about 7 ms. If any fragment is missing it falls back to live TTS for
  the whole reply, and logs it.
- **Tanglish replies are spoken by one Tamil voice:** English loanwords are written in Tamil script
  (alarm → அலாரம்), so there's no jarring switch between voices mid-sentence.
- **Voices:** Piper (VITS architecture) for English (`en_US-amy-low`) and a community Piper Tamil voice
  (ValluvarNeural) that we converted for sherpa-onnx ourselves (generated `tokens.txt` from its phoneme
  map and added the ONNX metadata). Piper converts text to phonemes with **espeak-ng**.

### 3.7 Chat path: local LLM

- **Model:** Qwen3-0.6B-Instruct, **GGUF Q4_K_M** (4-bit k-quant, about 4.8 bits per weight on average),
  ~380 MB. Thinking mode is disabled through the chat template.
- **Engine:** `llama-server` (llama.cpp's C++ HTTP server), `--threads 2 --ctx-size 1024 --parallel 1`,
  bound to localhost and supervised by Go (health check, restart). OpenAI-compatible endpoint with
  **SSE streaming**.
- **Prompt cache (KV cache reuse):** `cache_prompt: true`. The system prompt is processed once at
  startup (warm-up), and later turns only process the new tokens.
- **Phrase-level streaming TTS:** tokens are split into speakable chunks. The **first chunk is cut early
  (~4 words)** so speech starts while the LLM keeps generating; later chunks split on punctuation or about
  10 words.
- Answers are capped at **80 tokens**, and the system prompt asks for 1–2 short sentences with no
  markdown.

### 3.8 Graceful degradation

The controller polls the cgroup limits every 2 s, so `docker update` takes effect live:

| Tier | Trigger | LLM | Context |
|---|---|---|---|
| T0 | ≥ 2 cores and ≥ ~1.9 GB | default model | 1024 |
| T1 | < 1.9 GB | Qwen3-0.6B | 512 |
| T2 | < 1.2 GB or < 1.5 cores | Qwen3-0.6B | 256 |
| T3 | < 800 MB | **none**, commands + clips only; chat gets "I can only do quick commands right now" | – |

We removed latency-triggered tier switching because it **flapped** (one slow chat turn dropped to T1,
then the empty history jumped straight back to T0). Tiers now follow the enforced limits only.
⏳ *Live `docker update` validation and a larger T0 model (Llama-3.2-1B) are next.*

### 3.9 Measurement

- **Every stage is timestamped** with monotonic offsets from process start (never wall-clock time):
  speech start, last voiced frame, endpoint, ASR final, NLU done, LLM first token, first phrase, first
  audio out. Each turn is one JSON line in `results/`.
- **Memory:** cgroup `memory.current` sampled every 50 ms. We report **`anon`** (from `memory.stat`)
  as the honest *working set*. `memory.current` also counts the **page cache** of memory-mapped
  model files, which the kernel reclaims under pressure, so it inflates the number (~1.9 GB) without
  any real risk of running out of memory.
- **CPU:** cgroup `cpu.stat usage_usec` difference per turn = CPU-seconds per turn.
- **Energy:** ⏳ `powermetrics` (cpu_power sampler, 200 ms) on the host, integrating watts × time minus
  a 30 s idle baseline, giving J/turn.
- **Harness:** replays labelled WAVs through *the same live pipeline* in real time and reports p50/p95
  latency, intent accuracy, slot exact-match and CPU/RAM per config. Every optimisation is a config flag,
  so each ablation row is a YAML file.

---

## Numbers: say only these

| Metric | Measured | Notes |
|---|---|---|
| English command latency (end of speech → audio) | **~310 ms p50** (149-utterance synthetic set) | target < 500 ms ✅ |
| Tanglish command latency | **~510–620 ms** (live + replay) | slightly over 500 ms; the Tamil decode adds ~100–190 ms |
| Chat latency | **~2.1–2.2 s p50** | target < 1.0 s ✗. Breakdown: ~420 ms endpoint, ~500 ms LLM first token, ~250 ms first phrase, ~900 ms TTS |
| English intent accuracy | **96%** (synthetic set) | |
| Tanglish with the English recognizer only | **≈ 0% on real speech** | the motivating finding |
| Tanglish with dual ASR | correct on the developer's live Tanglish commands (alarm, date, time, cancel, greet, identity) | ⏳ formal real-speaker test set (20–30 utterances) |
| Working memory (anon) | **~1.1–1.2 GB** of 2 GB | stretch target < 1.5 GB ✅ |
| Model load at startup | **~3 s** (speech), LLM healthy in **~2 s** | |
| Baseline (Whisper → LLM → full reply → TTS) | ⏳ | config exists: `config/baseline.yaml` |
| Energy per turn | ⏳ | |

---

## Part 4: Likely judge questions

**Constraints and honesty**
1. *How do we know it's really CPU-only?* Linux container (no Metal), llama.cpp compiled with every GPU
   backend off, ONNX Runtime CPU provider only. NEON and fp16 are CPU instruction sets.
2. *How do we know it's really offline?* `--network=none`: the container has only a loopback interface.
   `make limits` shows it. The LLM talks to `127.0.0.1` inside the container.
3. *Is the limit real or just declared?* It's cgroup v2 enforced by the kernel. Swap equals memory,
   so there's no swap. We read `cpu.max` and `memory.max` from inside and show them.
4. *Why is "memory" 1.2 GB on the dashboard but higher in `docker stats`?* We show anonymous memory
   (the real working set). The cgroup total also counts reclaimable page cache from memory-mapped model
   files.
5. *How exactly do you measure latency?* From the last voiced audio frame (VAD) to the first reply audio
   leaving the container. That includes the endpointing wait, which many systems hide.

**ASR and Tanglish**
6. *Why two recognizers instead of one multilingual model?* The English streaming model gives instant
   partials and early endpointing, while the Tamil model gives Tamil accuracy. A multilingual Whisper isn't
   streaming and is weak on Tamil at small sizes.
7. *Doesn't running two ASRs double the CPU?* No. The English one streams during speech, and the Tamil
   one runs once, for ~100–190 ms, after the endpoint, in parallel with the English finalize.
8. *What is parse-based arbitration?* We let the downstream task pick the transcript: whichever one
   yields a valid command wins, so there's no confidence calibration across two different models.
9. *Why rules instead of an LLM for intent?* The command vocabulary is small and closed. Rules take
   microseconds, use no RAM, are deterministic and testable. A 0.6B LLM would add ~0.5–1 s and still make
   mistakes on Tanglish.
10. *What happens with an unseen phrasing?* The fuzzy and phonetic matching handles spelling variation.
    If it still doesn't parse, it goes to the LLM, so the user always gets an answer.
11. *What's CTC vs transducer?* See 3.4.
12. *What's the WER of the Tamil model?* The model card reports 33.5% WER / 16.7% CER on FLEURS Tamil
    (int8). For short commands, our parser only needs the key words, which is why intent accuracy is far
    higher than WER suggests.

**LLM and quantization**
13. *What is Q4_K_M?* llama.cpp's 4-bit k-quantization: weights in blocks with per-block scales,
    "M" = mixed precision keeps some sensitive tensors at higher bits. ~4× smaller than fp16, with a small
    quality loss.
14. *What's the prompt cache?* llama.cpp keeps the attention keys/values (KV cache) for the system
    prompt, so each turn only processes the new user tokens, which cuts time-to-first-token.
15. *Why is chat slower than the target?* On 2 cores, ~500 ms to the first token plus ~900 ms to
    synthesize the first phrase. Next fixes: a smaller first chunk, synthesizing while still generating
    (already partly done), and possibly a faster TTS for the first phrase.
16. *Why Qwen3-0.6B?* It fits the RAM budget next to two ASR models, and it's fast enough on 2 threads.
    Llama-3.2-1B is downloaded as the T0 option.

**System design**
17. *Why Go?* No interpreter overhead (~100–300 MB less than Python under a 2 GB cap), goroutines and
    channels fit a streaming pipeline, single binary. The heavy maths stays in C++ via sherpa-onnx (cgo)
    and llama.cpp.
18. *Why pre-synthesized clips?* TTS is the most expensive step for a short reply. Clips make the
    command reply audio near-free (~7 ms to stitch) and identical in voice to live TTS.
19. *What happens if the memory limit is cut live?* The degradation controller notices within 2 s and
    restarts llama-server smaller, or stops it (T3). Commands keep working from clips.
20. *Barge-in?* Deliberately half-duplex for the demo: laptop speakers would trigger the mic.
    Cancellation hooks exist (per-turn context).
21. *How did you test?* Table-driven unit tests for all pure logic (NLU, replies, actions, endpointer),
    a smoke-test "gate" inside the limited container, a 149-utterance synthetic set replayed through the
    live pipeline, and live recordings.

**Things to answer honestly if asked**
- Baseline comparison and energy: "being measured; here's the methodology" (3.9).
- Synthetic test audio inflates accuracy, which is why real recordings are reported separately.
- Tanglish *chat* is best-effort; only commands are guaranteed.
- The Tamil voice is a community model. Clips can be regenerated with a better offline Tamil TTS
  before judging.

---

## Part 5: Glossary

| Term | One-line meaning |
|---|---|
| **VAD** (voice activity detection) | Classifies each 32 ms frame as speech or not (Silero, a tiny neural net) |
| **Endpointing** | Deciding the user has finished speaking (silence timeout) |
| **Semantic endpointing** | Ending early because the words so far already form a complete command |
| **ASR** | Automatic speech recognition (speech → text) |
| **Streaming ASR** | Emits partial text while audio is still arriving |
| **Zipformer** | Efficient Conformer-style encoder used in k2/sherpa models |
| **Conformer** | Convolution + Transformer encoder, the standard for speech |
| **Transducer (RNN-T)** | ASR output model with an internal prediction network; streams naturally |
| **CTC** | Connectionist Temporal Classification: per-frame token/blank predictions, collapsed |
| **WER / CER** | Word / character error rate |
| **int8 dynamic quantization** | Store weights as 8-bit integers, quantize activations on the fly; ~4× smaller |
| **GGUF** | llama.cpp's model file format |
| **Q4_K_M** | 4-bit block quantization with mixed precision for sensitive tensors |
| **KV cache / prompt caching** | Reusing computed attention states for an unchanged prompt prefix |
| **TTFT** | Time to first token |
| **SSE** | Server-Sent Events: HTTP streaming used for LLM tokens and the dashboard |
| **TTS** | Text-to-speech |
| **VITS / Piper** | End-to-end neural TTS architecture / a fast VITS voice family |
| **espeak-ng** | Rule-based phonemizer that turns text into phonemes for Piper |
| **Code-mixing / Tanglish** | Mixing Tamil and English in one sentence |
| **Transliteration** | Converting script (Tamil → Latin letters) without translating |
| **Gemination** | Doubled consonants (ட்ட), pronounced voiceless ("tt") |
| **Levenshtein distance** | Minimum single-character edits between two strings |
| **Phonetic key** | Sound-alike normalisation (c→k, th→t, drop vowels) for matching |
| **Parse-based arbitration** | Picking between two transcripts by which one yields a valid command |
| **cgroup v2** | Linux kernel mechanism that enforces CPU/memory limits on a container |
| **cpu.max / memory.max** | The cgroup files holding the CPU quota and memory cap |
| **anon vs file memory** | Process heap/weights vs reclaimable page cache |
| **mmap** | Mapping a model file into memory; pages load on demand, counted as cache |
| **NEON** | ARM's CPU SIMD (vector) instructions |
| **Half-duplex** | Not listening while speaking |
| **Degradation tiers** | Pre-defined smaller configurations chosen as limits tighten |
| **Ablation** | Turning optimisations off one at a time to measure each one's contribution |
