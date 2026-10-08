//go:build linux

// gate: hour-2 smoke test inside the limited container. Prints GATE PASS if every component works.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"edgevoice/internal/audio"
	"edgevoice/internal/iface"
	"edgevoice/internal/llm"
	"edgevoice/internal/metrics"
	"edgevoice/internal/sherpa"
)

func main() {
	ok := true
	check := func(name string, err error, detail string) {
		if err != nil {
			ok = false
			fmt.Printf("FAIL %-12s %v\n", name, err)
			return
		}
		fmt.Printf("ok   %-12s %s\n", name, detail)
	}
	cores, mem := metrics.Limits("/sys/fs/cgroup")
	nets, _ := os.ReadDir("/sys/class/net")
	var ifs []string
	for _, n := range nets {
		ifs = append(ifs, n.Name())
	}
	fmt.Printf("limits: cores=%.1f mem=%dMB net=%v\n", cores, mem>>20, ifs)

	wav, err := audio.ReadWAV("/models/zipformer-en/test_wavs/0.wav")
	check("wav", err, fmt.Sprintf("%.1fs", float64(len(wav))/16000))

	vad, err := sherpa.NewVAD("/models/silero_vad.onnx")
	if check("vad", err, "loaded"); err == nil {
		speech := 0
		for i := 0; i+sherpa.VADWindow <= len(wav); i += sherpa.VADWindow {
			if vad.IsSpeech(audio.ToFloat(wav[i : i+sherpa.VADWindow])) {
				speech++
			}
		}
		fmt.Printf("     vad speech windows: %d\n", speech)
	}

	asr, err := sherpa.NewStreamingASR("/models/zipformer-en", 1, "", 0)
	if check("asr", err, "loaded"); err == nil {
		t := time.Now()
		asr.Accept(audio.ToFloat(wav))
		text := asr.Finalize()
		var e error
		if text == "" {
			e = fmt.Errorf("empty transcript")
		}
		check("asr-run", e, fmt.Sprintf("%q (%v)", text, time.Since(t).Round(time.Millisecond)))
	}

	for _, v := range []struct{ dir, text string }{{"/models/vits-piper-en_US-amy-low", "Gate passed."}, {"/models/piper-ta", "வணக்கம், நான் எட்ஜ்வாய்ஸ்"}} {
		tts, err := sherpa.NewTTS(v.dir, 1)
		if check("tts "+v.dir[8:], err, "loaded"); err == nil {
			t := time.Now()
			pcm := tts.Synth(v.text)
			var e error
			if len(pcm) == 0 {
				e = fmt.Errorf("no audio")
			}
			name := "/src/results/gate_" + strings.ReplaceAll(v.dir[8:], "/", "_") + ".wav"
			audio.WriteWAV(name, pcm)
			check("tts-run", e, fmt.Sprintf("%.1fs audio in %v -> %s", float64(len(pcm))/16000, time.Since(t).Round(time.Millisecond), name))
		}
	}

	srv := &llm.Server{Model: "/models/llm/Qwen3-0.6B-Q4_K_M.gguf", Threads: 2, Ctx: 512, Port: 8081}
	ctx := context.Background()
	t := time.Now()
	err = srv.Start(ctx)
	check("llm-start", err, fmt.Sprintf("healthy in %v", time.Since(t).Round(time.Millisecond)))
	if err == nil {
		c := &llm.Client{URL: srv.URL(), PromptCache: true, System: llm.SystemEN}
		t = time.Now()
		ch, err := c.Stream(ctx, []iface.Message{{Role: "user", Content: "Say hello in five words."}}, 24)
		var out strings.Builder
		var ttft time.Duration
		if err == nil {
			for tok := range ch {
				if ttft == 0 {
					ttft = time.Since(t)
				}
				out.WriteString(tok)
			}
		}
		check("llm-run", err, fmt.Sprintf("%q ttft=%v total=%v", strings.TrimSpace(out.String()), ttft.Round(time.Millisecond), time.Since(t).Round(time.Millisecond)))
		srv.Stop()
	}
	if peak, err := metrics.ReadInt("/sys/fs/cgroup/memory.peak"); err == nil {
		fmt.Printf("container memory.peak: %dMB\n", peak>>20)
	}
	if ok {
		fmt.Println("GATE PASS")
	} else {
		fmt.Println("GATE FAIL")
		os.Exit(1)
	}
}
