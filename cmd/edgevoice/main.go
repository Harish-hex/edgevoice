//go:build linux

// edgevoice: the on-device voice assistant runtime (runs inside the limited container).
//
//	edgevoice -config config/default.yaml -transport stdio          # live, audio over stdin/stdout frames
//	edgevoice -config config/default.yaml -transport sock           # live, audio over /sock/audio.sock
//	edgevoice -config config/default.yaml -replay data/recordings/synth   # harness replay of *.wav
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	_ "time/tzdata" // container may lack zoneinfo; TZ comes from the host via docker/run.sh

	"edgevoice/internal/audio"
	"edgevoice/internal/config"
	"edgevoice/internal/degrade"
	"edgevoice/internal/llm"
	"edgevoice/internal/metrics"
	"edgevoice/internal/nlu"
	"edgevoice/internal/pipeline"
	"edgevoice/internal/sherpa"
)

type frameOut struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (o *frameOut) PCM(p []int16) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(p) > 0 { // 100 ms frames
		n := min(len(p), 1600)
		audio.WriteFrame(o.w, audio.FramePCM, audio.PCMToBytes(p[:n]))
		p = p[n:]
	}
	o.w.Flush()
}
func (o *frameOut) Status(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	audio.WriteFrame(o.w, audio.FrameStatus, []byte(s))
	o.w.Flush()
}

func (o *frameOut) Event(kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["kind"] = kind
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	audio.WriteFrame(o.w, audio.FrameEvent, b)
	o.w.Flush()
}

type nullOut struct{}

func (nullOut) Event(string, map[string]any) {}

func (nullOut) PCM([]int16)     {}
func (nullOut) Status(s string) { log.Print(s) }

func main() {
	cfgPath := flag.String("config", "config/default.yaml", "config file")
	transport := flag.String("transport", "stdio", "stdio | sock")
	replay := flag.String("replay", "", "directory of .wav files to replay (harness mode)")
	limit := flag.Int("n", 0, "replay at most n files")
	dump := flag.Bool("dump", false, "save each turn's input audio to results/turns/")
	flag.Parse()
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	t0 := time.Now()
	p, srv := build(cfg)
	log.Printf("models loaded in %v (clips=%d)", time.Since(t0).Round(time.Millisecond), p.Clips.Len())
	ctx := context.Background()

	if srv != nil {
		go func() {
			if err := srv.Start(ctx); err != nil {
				log.Printf("LLM unavailable: %v", err)
				return
			}
			if err := p.LLM.Warm(ctx); err != nil {
				log.Printf("LLM warm: %v", err)
			}
			p.SetLLMReady(true)
			log.Printf("LLM ready (%s)", filepath.Base(cfg.Models.LLM))
		}()
		defer srv.Stop()
	}
	if cfg.Degrade && srv != nil {
		ctl := degrade.New(srv, p)
		go ctl.Run(ctx)
	}

	if *replay != "" {
		runReplay(ctx, p, *replay, *limit)
		return
	}
	var r io.Reader
	var w io.Writer
	switch *transport {
	case "stdio":
		r, w = os.Stdin, os.Stdout
	case "sock":
		os.Remove("/sock/audio.sock")
		ln, err := net.Listen("unix", "/sock/audio.sock")
		if err != nil {
			log.Fatal(err)
		}
		log.Print("waiting for audiobridge on /sock/audio.sock")
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		r, w = c, c
	}
	out := &frameOut{w: bufio.NewWriter(w)}
	p.Out = out
	p.HalfDuplex = true
	if *dump {
		p.DumpDir = "results/turns"
	}
	if cfg.Wake.Enabled {
		go liveSamples(ctx, p, srv, cfg)
		go p.RunScheduler(ctx)
		out.Status("EdgeVoice ready — say \"Hey Computer\", then your command (or both in one go).")
	} else {
		out.Status("EdgeVoice ready — speak.")
	}
	in := make(chan []int16, 64)
	go func() {
		defer close(in)
		br := bufio.NewReader(r)
		for {
			typ, payload, err := audio.ReadFrame(br)
			if err != nil {
				log.Printf("input closed: %v", err)
				return
			}
			if typ == audio.FramePCM {
				in <- audio.BytesToPCM(payload)
			}
		}
	}()
	p.Run(ctx, in)
}

// liveSamples streams resource usage to the dashboard every 500 ms (cgroup v2 + /proc).
func liveSamples(ctx context.Context, p *pipeline.Pipeline, srv *llm.Server, cfg *config.Config) {
	const cg = "/sys/fs/cgroup"
	prevCPU, _ := metrics.CPUUsec(cg)
	prevT := time.Now()
	for range time.Tick(500 * time.Millisecond) {
		cpu, _ := metrics.CPUUsec(cg)
		now := time.Now()
		cores := float64(cpu-prevCPU) / 1e6 / now.Sub(prevT).Seconds()
		prevCPU, prevT = cpu, now
		limCores, limMem := metrics.Limits(cg)
		memCur, _ := metrics.ReadInt(cg + "/memory.current")
		peak, _ := metrics.ReadInt(cg + "/memory.peak")
		ev := map[string]any{
			"cpu_cores": cores, "limit_cores": limCores, "limit_mem_mb": limMem >> 20,
			"mem_mb": memCur >> 20, "anon_mb": metrics.StatField(cg+"/memory.stat", "anon") >> 20, "peak_mb": peak >> 20,
			"rss_edgevoice_mb": metrics.ProcRSS(os.Getpid()) >> 20, "state": p.UIState(),
			"llm_ready": p.LLMReadyNow(), "llm_model": filepath.Base(cfg.Models.LLM), "net": "none",
			"wake": cfg.Wake.Enabled,
		}
		if srv != nil {
			ev["rss_llm_mb"] = metrics.ProcRSS(srv.PID()) >> 20
			ev["llm_model"] = filepath.Base(srv.Model)
		}
		if p.Tier != nil {
			ev["tier"] = p.Tier()
		}
		var sched []map[string]any
		for _, it := range p.State.Scheduled() {
			sched = append(sched, map[string]any{"kind": it.Kind, "label": it.Label, "at_ms": it.At.UnixMilli()})
		}
		ev["scheduled"] = sched
		ev["now_ms"] = time.Now().UnixMilli()
		p.Out.Event("sample", ev)
	}
}

func build(cfg *config.Config) (*pipeline.Pipeline, *llm.Server) {
	must := func(err error, what string) {
		if err != nil {
			log.Fatalf("%s: %v", what, err)
		}
	}
	lx := nlu.MustDefault()
	norm := nlu.NewNormalizer(lx)
	norm.Fuzzy, norm.Merge = cfg.NLU.Fuzzy, cfg.NLU.Merge
	p := &pipeline.Pipeline{Cfg: cfg, Norm: norm, Parser: nlu.NewParser(lx), Out: nullOut{}}
	var err error
	p.VAD, err = sherpa.NewVAD(cfg.P(cfg.Models.VAD))
	must(err, "vad")
	if cfg.ASR.Engine == "whisper" {
		p.Whisper, err = sherpa.NewWhisper(cfg.P(cfg.Models.WhisperDir), cfg.LLM.Threads)
		must(err, "whisper")
	} else {
		hw := ""
		if cfg.ASR.Hotwords {
			hw = "/src/clips/hotwords.txt"
		}
		p.ASR, err = sherpa.NewStreamingASR(cfg.P(cfg.Models.ASRDir), 1, hw, cfg.ASR.Score)
		must(err, "asr")
	}
	if cfg.ASR.Dual && cfg.Models.TamilDir != "" {
		if p.Tamil, err = sherpa.NewTamil(cfg.P(cfg.Models.TamilDir), cfg.LLM.Threads); err != nil {
			log.Printf("Tamil ASR unavailable (%v); English only", err)
			p.Tamil = nil
		}
	}
	p.TTSEn, err = sherpa.NewTTS(cfg.P(cfg.Models.TTSEn), 1)
	must(err, "tts en")
	if p.TTSTa, err = sherpa.NewTTS(cfg.P(cfg.Models.TTSTa), 1); err != nil {
		log.Printf("Tamil TTS unavailable (%v); using English voice", err)
		p.TTSTa = nil
	}
	p.Clips, err = pipeline.LoadClips(cfg.Reply.ClipsDir)
	if err != nil && cfg.Reply.Clips {
		log.Printf("clips unavailable (%v); live TTS only", err)
	}
	p.Bus, err = metrics.NewBus(cfg.Results, cfg.Name)
	must(err, "metrics")
	p.Bus.OnTurn = func(rec map[string]any) { p.Out.Event("turn", rec) } // p.Out is read at call time

	if !cfg.LLM.Enabled {
		return p, nil
	}
	srv := &llm.Server{Model: cfg.P(cfg.Models.LLM), Threads: cfg.LLM.Threads, Ctx: cfg.LLM.Ctx, Port: cfg.LLM.Port}
	p.LLM = &llm.Client{URL: srv.URL(), PromptCache: cfg.LLM.PromptCache, System: llm.SystemEN}
	return p, srv
}

// runReplay feeds each WAV in real time (+1.5 s trailing silence) and waits for its turn record.
func runReplay(ctx context.Context, p *pipeline.Pipeline, dir string, limit int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.wav"))
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	// Wait for the LLM (if any) so chat turns are measured warm.
	for i := 0; p.LLM != nil && i < 600 && !p.LLMReadyNow(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	in := make(chan []int16)
	go p.Run(ctx, in)
	silence := make([]int16, 512)
	for i, f := range files {
		pcm, err := audio.ReadWAV(f)
		if err != nil {
			log.Printf("skip %s: %v", f, err)
			continue
		}
		p.File = filepath.Base(f)
		pcm = append(pcm, make([]int16, audio.SampleRate*3/2)...)
		tick := time.NewTicker(32 * time.Millisecond)
		for off := 0; off < len(pcm); off += 512 {
			<-tick.C
			in <- pcm[off:min(off+512, len(pcm))]
		}
		tick.Stop()
		// let any reply finish before the next file
		for j := 0; j < 30; j++ {
			in <- silence
			time.Sleep(32 * time.Millisecond)
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", i+1, len(files), p.File)
	}
	close(in)
	time.Sleep(500 * time.Millisecond)
}
