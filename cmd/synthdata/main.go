//go:build linux

// synthdata generates the synthetic test set (DESIGN #3): EN and Tanglish command phrasings × slot values,
// spoken by the Piper English voice (romanized text) and the Piper Tamil voice (Tamil script), with
// speed and noise variation. Writes data/recordings/synth/*.wav + labels.jsonl.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"edgevoice/internal/audio"
	"edgevoice/internal/sherpa"
)

type num struct {
	n                          int
	ta, taScript, en, enScript string
}

var nums = []num{
	{1, "onnu", "ஒன்னு", "one", "ஒன்"}, {2, "rendu", "ரெண்டு", "two", "டூ"}, {3, "moonu", "மூணு", "three", "த்ரீ"},
	{4, "naalu", "நாலு", "four", "ஃபோர்"}, {5, "anju", "அஞ்சு", "five", "ஃபைவ்"}, {6, "aaru", "ஆறு", "six", "சிக்ஸ்"},
	{7, "ezhu", "ஏழு", "seven", "செவன்"}, {8, "ettu", "எட்டு", "eight", "எய்ட்"}, {9, "onbadhu", "ஒன்பது", "nine", "நைன்"},
	{10, "pathu", "பத்து", "ten", "டென்"}, {11, "padhinonnu", "பதினொன்னு", "eleven", "லெவன்"}, {12, "pannendu", "பன்னெண்டு", "twelve", "ட்வெல்வ்"},
}

type item struct {
	Mode   string            `json:"mode"`
	Roman  string            `json:"transcript_gold"`
	Script string            `json:"-"`
	Intent string            `json:"intent_gold"`
	Slots  map[string]string `json:"slots_gold"`
	File   string            `json:"file"`
	Voice  string            `json:"voice"`
	Speed  float32           `json:"speed"`
}

func gen() []item {
	var out []item
	add := func(mode, roman, script, intent string, slots map[string]string) {
		out = append(out, item{Mode: mode, Roman: roman, Script: script, Intent: intent, Slots: slots})
	}
	T, E := "TANGLISH", "ENGLISH"
	for _, n := range nums {
		hh := fmt.Sprintf("%02d:00", n.n)
		if n.n <= 11 && n.n >= 5 {
			add(T, "naalaikku kaalai "+n.ta+" manikku alarm vei", "நாளைக்கு காலை "+n.taScript+" மணிக்கு அலாரம் வை", "alarm.set", map[string]string{"day": "tomorrow", "time": hh})
			add(T, "naalaikku morning "+n.en+" ku alarm set pannu", "நாளைக்கு மார்னிங் "+n.enScript+" க்கு அலாரம் செட் பண்ணு", "alarm.set", map[string]string{"day": "tomorrow", "time": hh})
			add(E, "set an alarm for "+n.en+" a m tomorrow", "", "alarm.set", map[string]string{"day": "tomorrow", "time": hh})
			add(E, "wake me up at "+n.en+" tomorrow morning", "", "alarm.set", map[string]string{"day": "tomorrow", "time": hh})
		}
		if n.n >= 7 && n.n <= 11 {
			add(T, "raathiri "+n.ta+" manikku alarm vechidu", "ராத்திரி "+n.taScript+" மணிக்கு அலாரம் வெச்சிடு", "alarm.set", map[string]string{"time": fmt.Sprintf("%02d:00", n.n+12)})
		}
		add(T, n.ta+" nimisham timer vei", n.taScript+" நிமிஷம் டைமர் வை", "timer.set", map[string]string{"duration": fmt.Sprintf("%dm", n.n)})
		add(E, "set a timer for "+n.en+" minutes", "", "timer.set", map[string]string{"duration": fmt.Sprintf("%dm", n.n)})
		if n.n >= 2 && n.n <= 9 {
			m := nums[(n.n+3)%12]
			add(T, n.en+" into "+m.en+" evlo", n.enScript+" இன்டு "+m.enScript+" எவ்ளோ", "calc", map[string]string{"expression": fmt.Sprintf("%d*%d", n.n, m.n)})
			add(E, "what is "+n.en+" times "+m.en, "", "calc", map[string]string{"expression": fmt.Sprintf("%d*%d", n.n, m.n)})
		}
	}
	fixed := []struct{ mode, roman, script, intent string }{
		{T, "alarm cancel pannu", "அலாரம் கேன்சல் பண்ணு", "alarm.cancel"},
		{T, "alarm ah niruthu", "அலாரம நிறுத்து", "alarm.cancel"},
		{E, "cancel my alarm", "", "alarm.cancel"},
		{T, "time enna", "டைம் என்ன", "clock.time"},
		{T, "mani enna aachu", "மணி என்ன ஆச்சு", "clock.time"},
		{E, "what time is it", "", "clock.time"},
		{E, "what's the time now", "", "clock.time"},
		{T, "innaikku date enna", "இன்னைக்கு டேட் என்ன", "clock.date"},
		{E, "what's the date today", "", "clock.date"},
		{T, "vanakkam", "வணக்கம்", "smalltalk.greet"},
		{E, "hello", "", "smalltalk.greet"},
		{T, "nee yaaru", "நீ யாரு", "smalltalk.identity"},
		{E, "who are you", "", "smalltalk.identity"},
		{T, "naalaikku weather eppadi irukkum", "நாளைக்கு வெதர் எப்படி இருக்கும்", "offline.unsupported"},
		{E, "what's the weather tomorrow", "", "offline.unsupported"},
		{T, "niruthu", "நிறுத்து", "system.stop"},
		{E, "stop", "", "system.stop"},
		{E, "tell me a fun fact about space", "", "llm"},
		{E, "why is the sky blue", "", "llm"},
		{E, "give me one tip to sleep better", "", "llm"},
		{E, "what is the capital of france", "", "llm"},
		{E, "tell me a short joke", "", "llm"},
		{T, "oru joke sollu", "ஒரு ஜோக் சொல்லு", "llm"},
		{T, "enakku bore adikkudhu enna pannalaam", "எனக்கு போர் அடிக்குது என்ன பண்ணலாம்", "llm"},
		{T, "chennai pathi oru fact sollu", "சென்னை பத்தி ஒரு ஃபேக்ட் சொல்லு", "llm"},
	}
	for _, f := range fixed {
		add(f.mode, f.roman, f.script, f.intent, nil)
	}
	return out
}

func main() {
	outDir := flag.String("out", "data/recordings/synth", "output dir")
	max := flag.Int("n", 220, "max utterances")
	flag.Parse()
	en, err := sherpa.NewTTS("/models/vits-piper-en_US-amy-low", 2)
	if err != nil {
		log.Fatal(err)
	}
	ta, err := sherpa.NewTTS("/models/piper-ta", 2)
	if err != nil {
		log.Fatal(err)
	}
	rng := rand.New(rand.NewSource(42))
	var all []item
	for _, it := range gen() {
		// English voice reads the romanized text; Tanglish items also get the Tamil voice on Tamil script.
		a := it
		a.Voice = "en"
		all = append(all, a)
		if it.Script != "" {
			b := it
			b.Voice = "ta"
			all = append(all, b)
		}
	}
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	if len(all) > *max {
		all = all[:*max]
	}
	os.MkdirAll(*outDir, 0o755)
	lf, _ := os.Create(filepath.Join(*outDir, "labels.jsonl"))
	defer lf.Close()
	for i, it := range all {
		it.Speed = 0.9 + 0.25*rng.Float32()
		text := it.Roman
		tts := en
		if it.Voice == "ta" {
			text, tts = it.Script, ta
		}
		pcm := tts.SynthSpeed(text, it.Speed)
		lead := make([]int16, audio.SampleRate*3/10)
		pcm = append(lead, pcm...)
		for k := range pcm { // light white noise (~30 dB below speech peak)
			pcm[k] += int16(rng.NormFloat64() * 60)
		}
		it.File = fmt.Sprintf("%03d_%s_%s.wav", i, strings.ToLower(it.Mode[:2]), it.Voice)
		if err := audio.WriteWAV(filepath.Join(*outDir, it.File), pcm); err != nil {
			log.Fatal(err)
		}
		b, _ := json.Marshal(it)
		lf.Write(append(b, '\n'))
	}
	log.Printf("wrote %d utterances to %s", len(all), *outDir)
}
