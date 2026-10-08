//go:build linux

// buildclips pre-synthesizes every reply fragment with the runtime TTS voices (PRD §7.5, O8).
// Output: clips/<key>.pcm (16 kHz int16) + clips/manifest.json.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"edgevoice/internal/audio"
	"edgevoice/internal/config"
	"edgevoice/internal/pipeline"
	"edgevoice/internal/reply"
	"edgevoice/internal/sherpa"
)

func main() {
	cfgPath := flag.String("config", "config/default.yaml", "config")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	en, err := sherpa.NewTTS(cfg.P(cfg.Models.TTSEn), 2)
	if err != nil {
		log.Fatal(err)
	}
	ta, err := sherpa.NewTTS(cfg.P(cfg.Models.TTSTa), 2)
	if err != nil {
		log.Fatal(err)
	}
	dir := cfg.Reply.ClipsDir
	os.MkdirAll(dir, 0o755)
	man := map[string]string{}
	t0 := time.Now()
	frags := reply.AllFragments()
	for i, f := range frags {
		tts := en
		if f.Voice == "ta" {
			tts = ta
		}
		pcm := tts.Synth(f.Text)
		if len(pcm) == 0 {
			log.Printf("empty synth for %q", f.Text)
			continue
		}
		pcm = trim(pcm)
		key := pipeline.ClipKey(f)
		if err := os.WriteFile(filepath.Join(dir, key+".pcm"), audio.PCMToBytes(pcm), 0o644); err != nil {
			log.Fatal(err)
		}
		man[key] = key + ".pcm"
		if i%50 == 0 {
			log.Printf("%d/%d", i, len(frags))
		}
	}
	b, _ := json.MarshalIndent(man, "", " ")
	os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644)
	log.Printf("built %d clips in %v", len(man), time.Since(t0).Round(time.Second))
}

// trim removes leading/trailing near-silence so stitched replies don't have long gaps.
func trim(p []int16) []int16 {
	const th = 300
	s, e := 0, len(p)
	for s < e && abs(p[s]) < th {
		s++
	}
	for e > s && abs(p[e-1]) < th {
		e--
	}
	s = max(0, s-400)
	e = min(len(p), e+800)
	return p[s:e]
}

func abs(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}
