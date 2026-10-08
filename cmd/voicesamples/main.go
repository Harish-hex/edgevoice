//go:build linux

// voicesamples renders the same sentences with each candidate voice into results/voices/ and reports
// load time, synthesis speed (real-time factor) and process RSS, so voices can be compared by ear and cost.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"edgevoice/internal/audio"
	"edgevoice/internal/sherpa"
)

func rssMB() float64 {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "VmRSS:"); ok {
			var kb float64
			fmt.Sscan(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), &kb)
			return kb / 1024
		}
	}
	return 0
}

func main() {
	en := "Done, alarm set for six a.m. tomorrow. One fun fact about space: a day on Venus is longer than its year."
	ta := "சரி, நாளைக்கு காலை ஆறு மணிக்கு அலாரம் வெச்சிட்டேன். இப்போ மணி ரெண்டு."
	out := "results/voices"
	os.MkdirAll(out, 0o755)
	for _, v := range os.Args[1:] {
		text := en
		if strings.Contains(v, "-ta") {
			text = ta
		}
		r0 := rssMB()
		t0 := time.Now()
		tts, err := sherpa.NewTTS("/models/"+v, 2)
		if err != nil {
			fmt.Printf("%-36s FAIL %v\n", v, err)
			continue
		}
		load := time.Since(t0)
		t1 := time.Now()
		pcm := tts.Synth(text)
		synth := time.Since(t1)
		secs := float64(len(pcm)) / audio.SampleRate
		name := strings.NewReplacer("/", "_", "#", "_spk").Replace(v)
		audio.WriteWAV(filepath.Join(out, name+".wav"), pcm)
		fmt.Printf("%-36s load %5.0fms  synth %5.0fms for %4.1fs audio (RTF %.2f)  +RSS %4.0fMB  -> results/voices/%s.wav\n",
			v, float64(load.Milliseconds()), float64(synth.Milliseconds()), secs, synth.Seconds()/secs, rssMB()-r0, name)
	}
}
