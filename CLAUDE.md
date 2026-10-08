# EdgeVoice
Read `PRDcbt.md` (spec), then `docs/DESIGN.md` (confirmed overrides — these win), then
`docs/plans/2026-10-08-edgevoice-implementation.md` (task plan). Runtime is Go; Python only in tools/.
Never add network calls to the runtime or enable GPU/Metal/CoreML. Pure packages (nlu, reply, actions)
must not import cgo/model code. Use `make test` natively; cgo code builds only in the container (`make dev`).
GOPROXY=direct (proxy.golang.org TLS fails on this machine).
