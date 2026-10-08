# EdgeVoice

Offline, CPU-only English + Tanglish voice assistant running in a Docker container capped at
**2 CPUs / 2 GB RAM / no network**. Spec: `PRDcbt.md` · Design: `docs/DESIGN.md` ·
How it works: `docs/TECHNICAL_EXPLAINER.md`.

## Demo

```bash
make run                 # starts the container (2 CPU / 2 GB / --network=none) + mic/speaker bridge
```
Wait for `EdgeVoice ready — speak.` then talk. The first time, macOS asks for microphone access
for your terminal: allow it. Smaller limits (degradation demo):
```bash
make run CPUS=1 MEM=1g
docker update --cpus 1 --memory 768m --memory-swap 768m edgevoice   # tighten live, from another terminal
```

Try: "what time is it" · "set an alarm for six a m tomorrow" · "set a timer for five minutes" ·
"naalaikku kaalai aaru manikku alarm vei" · "anju nimisham timer vei" · "time enna" ·
"nee yaaru" · "vanakkam" · "tell me a fun fact about space".

Show judges the enforced limits: `make limits`.

## Setup (once)
```bash
bash tools/download_models.sh            # only script that uses the internet
tools/.venv/bin/python tools/convert_piper.py models/piper-ta/ta_IN-ValluvarNeural-medium.onnx
make image                               # CPU-only llama.cpp + Go toolchain image
make build gate clips                    # build binaries, smoke test, pre-synthesize reply clips
```

## Measuring
```bash
make synth                                # synthetic test set -> data/recordings/synth
CPUS=2 MEM=2g docker/run.sh bin/edgevoice -config config/ablations/10_full.yaml -replay data/recordings/synth
go run ./cmd/harness -labels data/recordings/synth/labels.jsonl results/*.jsonl
make test                                 # pure-Go unit tests (native, seconds)
```
