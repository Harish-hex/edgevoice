CPUS ?= 2
MEM  ?= 2g
RUN  := CPUS=$(CPUS) MEM=$(MEM) docker/run.sh

test:
	go vet ./internal/... && go test ./internal/...
image:
	docker build -f docker/Dockerfile -t edgevoice:dev .
dev:
	$(RUN) bash
limits:
	$(RUN) sh -c 'echo "cpu.max: $$(cat /sys/fs/cgroup/cpu.max)"; echo "memory.max: $$(cat /sys/fs/cgroup/memory.max)"; echo "net: $$(ls /sys/class/net)"'
gate:
	$(RUN) go run ./cmd/gate

.PHONY: test image dev limits gate clips run
clips:
	$(RUN) go run ./cmd/buildclips
run:
	go run ./cmd/audiobridge -cpus $(CPUS) -mem $(MEM)
