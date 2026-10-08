//go:build linux

// say: synthesize test audio. Usage: say <voice-dir> <out.wav> <text...>
package main

import (
	"log"
	"os"
	"strings"

	"edgevoice/internal/audio"
	"edgevoice/internal/sherpa"
)

func main() {
	tts, err := sherpa.NewTTS(os.Args[1], 2)
	if err != nil {
		log.Fatal(err)
	}
	pcm := append(make([]int16, 4800), tts.Synth(strings.Join(os.Args[3:], " "))...)
	if err := audio.WriteWAV(os.Args[2], pcm); err != nil {
		log.Fatal(err)
	}
}
