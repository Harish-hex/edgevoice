PURE := ./internal/iface/... ./internal/nlu/... ./internal/reply/... ./internal/actions/... ./internal/config/... ./internal/metrics/... ./internal/wire/... ./internal/pcm/... ./internal/endpoint/... ./internal/llm/... ./internal/tts/clause/... ./internal/degrade/...
CPUS ?= 2
MEM  ?= 2g
RUN  := CPUS=$(CPUS) MEM=$(MEM) docker/run.sh

test:
	go vet $(PURE) && go test $(PURE)
image:
	docker build -f docker/Dockerfile -t edgevoice:dev .
dev:
	$(RUN) bash
limits:
	$(RUN) sh -c 'echo "cpu.max: $$(cat /sys/fs/cgroup/cpu.max)"; echo "memory.max: $$(cat /sys/fs/cgroup/memory.max)"; echo "net: $$(ls /sys/class/net)"'
gate:
	$(RUN) go run ./cmd/gate

.PHONY: test image dev limits gate
