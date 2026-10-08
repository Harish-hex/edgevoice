//go:build linux

package degrade

import (
	"context"
	"log"
	"sync"
	"time"

	"edgevoice/internal/llm"
	"edgevoice/internal/metrics"
	"edgevoice/internal/pipeline"
)

// Controller polls cgroup limits every 2 s (so `docker update` is noticed live) and switches tiers by
// restarting llama-server with a smaller model/context, or stopping it (T3: commands + clips only).
type Controller struct {
	srv   *llm.Server
	p     *pipeline.Pipeline
	tiers map[string]Tier

	mu   sync.Mutex
	cur  string
	e2es []float64
}

func New(srv *llm.Server, p *pipeline.Pipeline) *Controller {
	small := p.Cfg.P("llm/Qwen3-0.6B-Q4_K_M.gguf")
	c := &Controller{srv: srv, p: p, cur: "T0", tiers: map[string]Tier{
		"T0": {"T0", srv.Model, p.Cfg.LLM.Ctx},
		"T1": {"T1", small, 512},
		"T2": {"T2", small, 256},
		"T3": {"T3", "", 0},
	}}
	p.Tier = c.Current
	prev := p.Bus.OnTurn
	p.Bus.OnTurn = func(rec map[string]any) {
		if prev != nil {
			prev(rec)
		}
		if v, ok := rec["e2e_ms"].(float64); ok {
			c.mu.Lock()
			c.e2es = append(c.e2es, v)
			if len(c.e2es) > 10 {
				c.e2es = c.e2es[1:]
			}
			c.mu.Unlock()
		}
	}
	return c
}

func (c *Controller) Current() string { c.mu.Lock(); defer c.mu.Unlock(); return c.cur }

func (c *Controller) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		cores, mem := metrics.Limits("/sys/fs/cgroup")
		c.mu.Lock()
		p95 := P95(c.e2es)
		cur := c.cur
		c.mu.Unlock()
		// Live demo: tier follows the enforced limits only (latency-triggered switching flapped: one slow
		// chat turn → T1, then empty history → T0). p95 is still logged.
		want := Select(cores, mem, 0)
		if want == cur {
			continue
		}
		log.Printf("degrade: %s -> %s (cores=%.1f mem=%dMB p95=%.0fms)", cur, want, cores, mem/MB, p95)
		c.mu.Lock()
		c.cur, c.e2es = want, nil
		c.mu.Unlock()
		c.apply(ctx, c.tiers[want])
	}
}

func (c *Controller) apply(ctx context.Context, t Tier) {
	c.p.SetLLMReady(false)
	c.srv.Stop()
	if t.Model == "" {
		c.p.Out.Status("Tier " + t.Name + ": commands only")
		return
	}
	c.srv.Model, c.srv.Ctx = t.Model, t.Ctx
	if err := c.srv.Start(ctx); err != nil {
		log.Printf("degrade: llm restart failed: %v", err)
		return
	}
	c.p.LLM.Warm(ctx)
	c.p.SetLLMReady(true)
	c.p.Out.Status("Tier " + t.Name + " active")
}
