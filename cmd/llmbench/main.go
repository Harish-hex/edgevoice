//go:build linux

// llmbench: same questions to each GGUF model under the container limits; reports time-to-first-token,
// total time, llama-server RSS and the answers (for judging quality by eye). Usage: llmbench model.gguf…
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"edgevoice/internal/iface"
	"edgevoice/internal/llm"
	"edgevoice/internal/metrics"
)

var questions = []string{
	"what is the capital of india?",
	"who is the prime minister of india?",
	"who is donald trump?",
	"tell me a fact about chennai",
	"who is the president of united state?",
	"why is the sky blue?",
	"tell me a short joke",
	"what is the tallest mountain in the world?",
	"how many days are in a year?",
	"who wrote thirukkural?",
	"tell me a fun fact about space",
	"what is photosynthesis?",
}

func main() {
	ctx := context.Background()
	for _, model := range os.Args[1:] {
		srv := &llm.Server{Model: model, Threads: 2, Ctx: 1024, Port: 8091}
		t0 := time.Now()
		if err := srv.Start(ctx); err != nil {
			fmt.Println("FAIL", model, err)
			continue
		}
		c := &llm.Client{URL: srv.URL(), PromptCache: true, System: llm.SystemEN}
		c.Warm(ctx)
		fmt.Printf("\n## %s (load %.1fs)\n", filepath.Base(model), time.Since(t0).Seconds())
		var ttfts []float64
		for _, q := range questions {
			start := time.Now()
			ch, err := c.Stream(ctx, []iface.Message{{Role: "user", Content: q}}, 80)
			if err != nil {
				fmt.Println("ERR", err)
				continue
			}
			var b strings.Builder
			var ttft time.Duration
			for tok := range ch {
				if ttft == 0 {
					ttft = time.Since(start)
				}
				b.WriteString(tok)
			}
			ttfts = append(ttfts, float64(ttft.Milliseconds()))
			ans := strings.Join(strings.Fields(b.String()), " ")
			fmt.Printf("- [%4dms first token, %5.1fs total] %s\n    → %s\n", ttft.Milliseconds(), time.Since(start).Seconds(), q, ans)
		}
		sum := 0.0
		for _, v := range ttfts {
			sum += v
		}
		fmt.Printf("mean first-token: %.0f ms · llama-server RSS: %d MB · container memory.peak: %d MB\n",
			sum/float64(len(ttfts)), metrics.ProcRSS(srv.PID())>>20, func() int64 { v, _ := metrics.ReadInt("/sys/fs/cgroup/memory.peak"); return v >> 20 }())
		srv.Stop()
	}
}
