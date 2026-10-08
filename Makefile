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
	$(RUN) sh -c 'echo "cpu.max: $$(cat /sys/fs/cgroup/cpu.max)"; echo "memory.max: $$(cat /sys/fs/cgroup/memory.max)"; echo "net: $$(cat /sys/class/net/*/operstate | paste -sd, -) (only lo is up)"'
build:
	CPUS=4 MEM=3g docker/run.sh go build -o bin/ ./cmd/edgevoice ./cmd/gate ./cmd/buildclips ./cmd/synthdata
gate: build
	$(RUN) bin/gate

.PHONY: test image dev limits build gate clips synth run
clips: build
	CPUS=4 MEM=3g docker/run.sh bin/buildclips
synth: build
	CPUS=4 MEM=3g docker/run.sh bin/synthdata
run:
	go run ./cmd/audiobridge -cpus $(CPUS) -mem $(MEM)
