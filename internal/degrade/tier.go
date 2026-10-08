// Package degrade picks a resource tier (PRD §7.8) from live cgroup limits and recent latency.
package degrade

import (
	"math"
	"sort"
)

const MB = 1 << 20

type Tier struct {
	Name    string
	Model   string // "" => no LLM (commands only)
	Ctx     int
	Threads int
}

// Select implements the tier table. cores/mem of 0 mean "unlimited".
func Select(cores float64, mem int64, p95ms float64) string {
	switch {
	case mem > 0 && mem < 800*MB:
		return "T3"
	case (mem > 0 && mem < 1200*MB) || (cores > 0 && cores < 1.5):
		return "T2"
	case (mem > 0 && mem < 1900*MB) || p95ms > 1500:
		return "T1"
	}
	return "T0"
}

// P95 (nearest-rank).
func P95(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(math.Ceil(0.95*float64(len(s))))-1]
}
