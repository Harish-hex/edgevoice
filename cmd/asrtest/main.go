//go:build linux

// asrtest: print both recognizers' transcripts and raw confidence data for WAV files.
package main

import (
	"fmt"
	"os"

	so "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"edgevoice/internal/audio"
	"edgevoice/internal/sherpa"
)

func main() {
	ta, err := sherpa.NewTamil("/models/indicconformer-ta", 2)
	if err != nil {
		panic(err)
	}
	en, err := sherpa.NewStreamingASR("/models/zipformer-en", 1, "", 0)
	if err != nil {
		panic(err)
	}
	for _, f := range os.Args[1:] {
		pcm, _ := audio.ReadWAV(f)
		x := audio.ToFloat(audio.TrimSilence(pcm, 50))
		r := ta.Result(x)
		fmt.Printf("== %s\nTA %q\n   tokens=%v\n   logprobs=%v\n", f, r.Text, r.Tokens, r.YsLogProbs)
		en.Accept(audio.ToFloat(pcm))
		fmt.Printf("EN json=%s\n", en.FinalJSON())
	}
	_ = so.OfflineRecognizerResult{}
}
