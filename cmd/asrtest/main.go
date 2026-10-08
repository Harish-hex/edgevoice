//go:build linux

// asrtest: run the Tamil recognizer on WAV files with different amounts of leading/trailing silence.
package main

import (
	"fmt"
	"os"

	"edgevoice/internal/audio"
	"edgevoice/internal/sherpa"
)

func main() {
	ta, err := sherpa.NewTamil("/models/indicconformer-ta", 2)
	if err != nil {
		panic(err)
	}
	for _, f := range os.Args[1:] {
		pcm, _ := audio.ReadWAV(f)
		// strip leading near-silence, then re-pad with various amounts
		s := 0
		for s < len(pcm) && pcm[s] < 300 && pcm[s] > -300 {
			s++
		}
		core := pcm[s:]
		for _, lead := range []int{0, 100, 300, 600} {
			for _, tail := range []int{0, 500} {
				x := append(append(make([]int16, 16*lead), core...), make([]int16, 16*tail)...)
				fmt.Printf("%s lead=%dms tail=%dms: %s\n", f, lead, tail, ta.Transcribe(audio.ToFloat(x)))
			}
		}
	}
}
