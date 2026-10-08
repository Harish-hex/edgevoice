// harness summarizes replay runs: joins per-turn JSONL (results/<config>-*.jsonl) with labels.jsonl and
// prints a markdown table (PRD §10.3). Runs natively on the host.
//
//	go run ./cmd/harness -labels data/recordings/synth/labels.jsonl results/full-*.jsonl results/baseline-*.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type label struct {
	File, Mode, Intent string
	Slots              map[string]string
}

func readJSONL(path string, fn func(map[string]any)) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			fn(m)
		}
	}
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(math.Ceil(p*float64(len(s))))-1]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	t := 0.0
	for _, x := range xs {
		t += x
	}
	return t / float64(len(xs))
}

func main() {
	labelsPath := flag.String("labels", "data/recordings/synth/labels.jsonl", "labels.jsonl")
	real := flag.Bool("real", false, "real-voice scoring: commands / questions / noise rejection / wrong actions")
	verbose := flag.Bool("v", false, "print per-turn results (with -real)")
	flag.Parse()
	labels := map[string]label{}
	readJSONL(*labelsPath, func(m map[string]any) {
		l := label{File: fmt.Sprint(m["file"]), Mode: fmt.Sprint(m["mode"]), Intent: fmt.Sprint(m["intent_gold"]), Slots: map[string]string{}}
		if s, ok := m["slots_gold"].(map[string]any); ok {
			for k, v := range s {
				l.Slots[k] = fmt.Sprint(v)
			}
		}
		labels[l.File] = l
	})

	if *real {
		fmt.Println("| run | n | commands correct | questions → LLM | noise rejected | wake | wrong actions | nonsense chats |")
		fmt.Println("|---|---|---|---|---|---|---|---|")
		var details []string
		for _, path := range flag.Args() {
			var recs []map[string]any
			readJSONL(path, func(m map[string]any) { recs = append(recs, m) })
			s := scoreReal(labels, recs)
			name := filepath.Base(path)
			fmt.Printf("| %s | %d | %s | %s | %s | %s | %d | %d |\n", name, s.n, pctS(s.cmdOK, s.cmdN), pctS(s.qOK, s.qN),
				pctS(s.noiseOK, s.noiseN), pctS(s.wakeOK, s.wakeN), s.wrongAction, s.nonsense)
			if *verbose {
				details = append(details, "\n### "+name)
				details = append(details, s.rows...)
			}
		}
		for _, d := range details {
			fmt.Println(d)
		}
		return
	}
	fmt.Println("| config | n | intent acc EN | intent acc TA | slot EM | e2e p50 cmd (ms) | e2e p95 cmd | e2e p50 llm | e2e p95 llm | CPU-s/turn | peak mem MB (anon) |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|---|")
	for _, path := range flag.Args() {
		name := strings.SplitN(filepath.Base(path), "-", 2)[0]
		var n, okEN, nEN, okTA, nTA, slotOK, slotN int
		var cmdE2E, llmE2E, cpu []float64
		peak, peakAnon := 0.0, 0.0
		seen := map[string]bool{}
		readJSONL(path, func(m map[string]any) {
			file := fmt.Sprint(m["file"])
			l, ok := labels[file]
			if !ok || seen[file] {
				return // ignore unlabeled or duplicate turns (e.g. noise-triggered)
			}
			seen[file] = true
			n++
			got := fmt.Sprint(m["intent"])
			hit := got == l.Intent
			if l.Mode == "TANGLISH" {
				nTA++
				if hit {
					okTA++
				}
			} else {
				nEN++
				if hit {
					okEN++
				}
			}
			if hit && len(l.Slots) > 0 {
				slotN++
				gs, _ := m["slots"].(map[string]any)
				all := true
				for k, v := range l.Slots {
					if fmt.Sprint(gs[k]) != v {
						all = false
					}
				}
				if all {
					slotOK++
				}
			}
			if e, ok := m["e2e_ms"].(float64); ok {
				if m["route"] == "llm" {
					llmE2E = append(llmE2E, e)
				} else {
					cmdE2E = append(cmdE2E, e)
				}
			}
			if c, ok := m["cpu_s"].(float64); ok {
				cpu = append(cpu, c)
			}
			if p, ok := m["peak_mem_mb"].(float64); ok && p > peak {
				peak = p
			}
			if p, ok := m["peak_anon_mb"].(float64); ok && p > peakAnon {
				peakAnon = p
			}
		})
		fr := func(a, b int) string {
			if b == 0 {
				return "–"
			}
			return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(a)/float64(b), a, b)
		}
		fmt.Printf("| %s | %d | %s | %s | %s | %.0f | %.0f | %.0f | %.0f | %.2f | %.0f (%.0f) |\n", name, n, fr(okEN, nEN), fr(okTA, nTA), fr(slotOK, slotN),
			pct(cmdE2E, .5), pct(cmdE2E, .95), pct(llmE2E, .5), pct(llmE2E, .95), mean(cpu), peak, peakAnon)
	}
}
