// Package metrics records per-turn timestamps (monotonic offsets) and container resource usage (cgroup v2).
package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var start = time.Now()

// Now is the monotonic offset since process start (PRD §6: never wall clock).
func Now() time.Duration { return time.Since(start) }

type Turn struct {
	ID     int                      `json:"turn"`
	Marks  map[string]time.Duration `json:"-"`
	Fields map[string]any           `json:"-"`
	cpu0   int64
}

// Bus collects turns and appends one JSON line per finished turn.
type Bus struct {
	mu     sync.Mutex
	f      *os.File
	cg     string
	peak   int64
	nextID int
	OnTurn func(rec map[string]any) // optional hook (status line, degrade controller)
}

func NewBus(resultsDir, runName string) (*Bus, error) {
	os.MkdirAll(resultsDir, 0o755)
	f, err := os.Create(filepath.Join(resultsDir, runName+"-"+time.Now().Format("150405")+".jsonl"))
	if err != nil {
		return nil, err
	}
	b := &Bus{f: f, cg: "/sys/fs/cgroup"}
	go b.sample()
	return b, nil
}

// sample tracks peak container memory every 50 ms.
func (b *Bus) sample() {
	for range time.Tick(50 * time.Millisecond) {
		if v, err := ReadInt(b.cg + "/memory.current"); err == nil {
			b.mu.Lock()
			if v > b.peak {
				b.peak = v
			}
			b.mu.Unlock()
		}
	}
}

func (b *Bus) Start() *Turn {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.peak = 0
	b.mu.Unlock()
	t := &Turn{ID: id, Marks: map[string]time.Duration{}, Fields: map[string]any{}}
	t.cpu0, _ = CPUUsec(b.cg)
	return t
}

func (t *Turn) Mark(name string) { t.MarkAt(name, Now()) }
func (t *Turn) MarkAt(name string, at time.Duration) {
	if _, ok := t.Marks[name]; !ok {
		t.Marks[name] = at
	}
}
func (t *Turn) Set(k string, v any) { t.Fields[k] = v }
func ms(d time.Duration) float64    { return float64(d.Microseconds()) / 1000 }

// End writes the turn record. e2e = t_first_audio_out − t_last_voiced (PRD §2.5).
func (b *Bus) End(t *Turn) map[string]any {
	rec := map[string]any{"turn": t.ID}
	for k, v := range t.Fields {
		rec[k] = v
	}
	for k, v := range t.Marks {
		rec[k+"_ms"] = ms(v)
	}
	if out, ok := t.Marks["t_first_audio_out"]; ok {
		if lv, ok := t.Marks["t_last_voiced"]; ok {
			rec["e2e_ms"] = ms(out - lv)
		}
	}
	if c1, err := CPUUsec(b.cg); err == nil {
		rec["cpu_s"] = float64(c1-t.cpu0) / 1e6
	}
	b.mu.Lock()
	rec["peak_mem_mb"] = float64(b.peak) / (1 << 20)
	line, _ := json.Marshal(rec)
	b.f.Write(append(line, '\n'))
	b.mu.Unlock()
	if b.OnTurn != nil {
		b.OnTurn(rec)
	}
	return rec
}

func ReadInt(path string) (int64, error) {
	s, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(s)), 10, 64)
}

// CPUUsec reads usage_usec from cpu.stat under the cgroup root.
func CPUUsec(root string) (int64, error) {
	s, err := os.ReadFile(root + "/cpu.stat")
	if err != nil {
		return 0, err
	}
	for _, l := range strings.Split(string(s), "\n") {
		if v, ok := strings.CutPrefix(l, "usage_usec "); ok {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, os.ErrNotExist
}

// Limits returns (cores, memBytes) from cpu.max / memory.max; 0 means unlimited.
func Limits(root string) (float64, int64) {
	var cores float64
	if s, err := os.ReadFile(root + "/cpu.max"); err == nil {
		f := strings.Fields(string(s))
		if len(f) == 2 && f[0] != "max" {
			q, _ := strconv.ParseFloat(f[0], 64)
			p, _ := strconv.ParseFloat(f[1], 64)
			if p > 0 {
				cores = q / p
			}
		}
	}
	mem, _ := ReadInt(root + "/memory.max")
	return cores, mem
}
