# Stage 1: llama-server, CPU-only. Linux has no Metal; GPU/BLAS backends are also disabled explicitly.
FROM debian:bookworm-slim AS llama
RUN apt-get update && apt-get install -y --no-install-recommends build-essential cmake git ca-certificates && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 https://github.com/ggml-org/llama.cpp /llama
WORKDIR /llama
RUN cmake -B build -DGGML_METAL=OFF -DGGML_BLAS=OFF -DGGML_CUDA=OFF -DGGML_VULKAN=OFF -DGGML_NATIVE=ON \
      -DLLAMA_CURL=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON \
      -DBUILD_SHARED_LIBS=OFF -DCMAKE_BUILD_TYPE=Release \
 && cmake --build build --target llama-server -j

# Stage 2: Go toolchain (cgo for sherpa-onnx). Go modules come from the host's module cache (mounted).
FROM golang:1.24-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends procps && rm -rf /var/lib/apt/lists/*
COPY --from=llama /llama/build/bin/llama-server /usr/local/bin/llama-server
ENV CGO_ENABLED=1 GOFLAGS=-buildvcs=false GOPROXY=off GOTOOLCHAIN=local
WORKDIR /src
CMD ["bash"]
