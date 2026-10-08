#!/usr/bin/env bash
# Downloads all models into ./models and records SHA256s. The ONLY script allowed to use the network.
set -uo pipefail
cd "$(dirname "$0")/../models"
GH=https://github.com/k2-fsa/sherpa-onnx/releases/download
HF=https://huggingface.co
fetch() { [ -s "$2" ] || curl -fsSL --retry 3 -o "$2" "$1" || { echo "FAIL $1"; rm -f "$2"; return 1; }; echo "ok $2"; }
getx() { fetch "$GH/$1/$2.tar.bz2" "$2.tar.bz2" && { [ -d "$2" ] || tar xjf "$2.tar.bz2"; }; }

fetch $GH/asr-models/silero_vad.onnx silero_vad.onnx
getx asr-models sherpa-onnx-streaming-zipformer-en-2023-06-26
getx asr-models sherpa-onnx-whisper-base
getx tts-models vits-piper-en_US-amy-low
getx tts-models vits-mms-tam || echo "WARN no prebuilt vits-mms-tam"
mkdir -p llm && cd llm
fetch $HF/unsloth/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf Qwen3-0.6B-Q4_K_M.gguf
fetch $HF/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf Llama-3.2-1B-Instruct-Q4_K_M.gguf
fetch $HF/unsloth/gemma-3-1b-it-GGUF/resolve/main/gemma-3-1b-it-Q4_K_M.gguf gemma-3-1b-it-Q4_K_M.gguf
cd .. && find . -type f \( -name '*.onnx' -o -name '*.gguf' \) -exec shasum -a 256 {} \; > manifest.txt
echo DONE
