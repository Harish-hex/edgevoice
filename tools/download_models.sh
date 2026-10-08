#!/usr/bin/env bash
# Downloads models into ./models and records SHA256s. The ONLY script allowed to use the network.
# Fetches individual int8 files (not full release tarballs) — slow-network friendly, resumable (-C -).
set -uo pipefail
cd "$(dirname "$0")/../models"
GH=https://github.com/k2-fsa/sherpa-onnx/releases/download
HF=https://huggingface.co
fetch() { mkdir -p "$(dirname "$2")"; [ -s "$2.done" ] && return 0; curl -fsSL --http1.1 --retry 10 --retry-all-errors -C - -o "$2" "$1" && touch "$2.done" && echo "ok $2" || echo "FAIL $1"; }

# P1: gate essentials (VAD, English ASR, English TTS, smallest LLM)
fetch $GH/asr-models/silero_vad.onnx silero_vad.onnx
Z=$HF/csukuangfj/sherpa-onnx-streaming-zipformer-en-2023-06-26/resolve/main; D=zipformer-en
for f in encoder-epoch-99-avg-1-chunk-16-left-128.int8.onnx decoder-epoch-99-avg-1-chunk-16-left-128.onnx joiner-epoch-99-avg-1-chunk-16-left-128.int8.onnx tokens.txt bpe.model test_wavs/0.wav; do fetch $Z/$f $D/$f & done
( fetch $GH/tts-models/vits-piper-en_US-amy-low.tar.bz2 piper.tar.bz2 && { [ -d vits-piper-en_US-amy-low ] || tar xjf piper.tar.bz2; } ) &
fetch $HF/unsloth/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf llm/Qwen3-0.6B-Q4_K_M.gguf &
wait; echo P1_DONE
# English word list (frequency-ordered) for Tamil-script loanword back-transliteration, plus names
fetch https://raw.githubusercontent.com/first20hours/google-10000-english/master/google-10000-english-usa-no-swears.txt english-10k.txt
{ cat english-10k.txt; printf '%s\n' india tamil chennai madurai coimbatore delhi mumbai bangalore kerala modi trump donald biden obama gandhi kohli dhoni sachin messi ronaldo elon musk tesla rajinikanth vijay ajith kamal rahman ilaiyaraaja america china japan london paris dubai singapore cricket football bollywood kollywood netflix youtube instagram whatsapp spotify iphone android chatgpt ai; } > english-words.txt
# P2: Tamil TTS, baseline Whisper
M=$HF/willwade/mms-tts-multilingual-models-onnx/resolve/main/tam
W=$HF/csukuangfj/sherpa-onnx-whisper-base/resolve/main; D=whisper-base
for f in base-encoder.int8.onnx base-decoder.int8.onnx base-tokens.txt; do fetch $W/$f $D/$f & done
wait; echo P2_DONE
# P3: bigger LLM for T0
fetch $HF/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf llm/Llama-3.2-1B-Instruct-Q4_K_M.gguf
find . -type f \( -name '*.onnx' -o -name '*.gguf' \) -exec shasum -a 256 {} \; > manifest.txt
echo DONE
