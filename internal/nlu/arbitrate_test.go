package nlu

import (
	"strings"
	"testing"
	"time"

	"edgevoice/internal/iface"
)

// cand builds a candidate; lp/tpw are the English confidence features (lp=0 → unknown).
func cand(src, text string, lp, tpw float64) Candidate {
	lx := MustDefault()
	p := NewParser(lx)
	p.Now = func() time.Time { return testNow }
	n := NewNormalizer(lx).Normalize(iface.Transcript{Text: text})
	c := Candidate{Source: src, Text: text, Norm: n, Intent: p.Parse(n)}
	if lp != 0 {
		c.HasConf, c.AvgLogProb, c.TokensPerWord = true, lp, tpw
	}
	return c
}

func TestChoose(t *testing.T) {
	lx := MustDefault()
	lx.LoadEnglish(stringsReader("prime\nminister\nunited\nstates\npresident\ndonald\ntrump\nindia\nmonster\n"))
	cases := []struct {
		name             string
		en               Candidate
		ta               Candidate
		ok               bool
		wantSrc, wantInt string
		wantText         string
	}{
		{"tanglish command: EN garbage, TA parses", cand("en", "not like you call a lot of money colorambi", -1.3, 2.1), cand("ta", "நாளைக்கு காலை ஆறு மணிக்கு அலாரம் வை", 0, 0), true, "ta", "alarm.set", ""},
		{"english command", cand("en", "what time is it", -0.27, 1.0), cand("ta", "வாட் டைம் இஸ் இட்", 0, 0), true, "en", "clock.time", ""},
		{"english chat", cand("en", "tell me a fun fact about space", -0.2, 1.0), cand("ta", "டெல் மீ எ ஃபன் ஃபேக்ட் அபவுட் ஸ்பேஸ்", 0, 0), true, "en", "", "tell me a fun fact about space"},
		{"tanglish question: loanwords restored", cand("en", "but i am in a study ardi", -1.4, 1.9), cand("ta", "பிரைம் மினிஸ்டர் யாரு", 0, 0), true, "ta", "", "prime minister yaaru"},
		{"tanglish question 2", cand("en", "donal tramp yarde", -1.2, 2.3), cand("ta", "டொனால்ட் ட்ரம்ப் யாரு", 0, 0), true, "ta", "", "donald trump yaaru"},
		{"noise → unclear", cand("en", "bess it on him", -1.1, 1.8), cand("ta", "பேச நானேப்ப", 0, 0), false, "", "", ""},
		{"one-word → unclear", cand("en", "", 0, 0), cand("ta", "என்னது", 0, 0), false, "", "", ""},
		{"low-confidence EN false command rejected", cand("en", "they in violet", -1.6, 2.0), cand("ta", "தே இன் வயலட்", 0, 0), false, "", "", ""},
		{"accented real command kept", cand("en", "open spottifay", -0.84, 3.5), cand("ta", "ஓபன் பா்டட்டிஃைவ", 0, 0), true, "en", "app.open", ""},
		{"fragment is not a question", cand("en", "are now open", -0.76, 1.0), cand("ta", "நான் ஓபன்", 0, 0), false, "", "", ""},
		{"failed calc is not a question", cand("en", "one place will", -1.2, 1.5), cand("ta", "ஒன் ப்ளஸ் வில்", 0, 0), false, "", "", ""},
		{"real English question kept (BPE splits words)", cand("en", "how many planets are there in the solar system", -0.42, 2.1), cand("ta", "ஹவ் மெினி பிளானர்்ஸ் ஆர் தேர் இன் த சோலா சிஸ்டம்", 0, 0), true, "en", "", "how many planets are there in the solar system"},
		{"failed English command is not chat", cand("en", "for five minutes an hour", -0.52, 2.0), cand("ta", "செட்டனெல்லாம் ஃபார் ஃபைவ் மினிட்ஸ் என்ண்ணா", 0, 0), false, "", "", ""},
		{"untranslatable Tamil word: no guess", cand("en", "", 0, 0), cand("ta", "சரிஸ் என்ன பச்சிருக்காடா", 0, 0), false, "", "", ""},
		{"no subject is not a question", cand("en", "and a thought", -1.3, 1.5), cand("ta", "என்ன ஆது", 0, 0), false, "", "", ""},
	}
	for _, c := range cases {
		got, ok := lx.Choose([]Candidate{c.en, c.ta})
		intent := ""
		if got.Intent != nil {
			intent = got.Intent.Name
		}
		if ok != c.ok || (ok && (got.Source != c.wantSrc || intent != c.wantInt)) || (c.wantText != "" && got.Norm.Canonical != c.wantText && got.Text != c.wantText) {
			t.Errorf("%s: got ok=%v %s/%q text=%q; want ok=%v %s/%q text=%q", c.name, ok, got.Source, intent, got.Norm.Canonical, c.ok, c.wantSrc, c.wantInt, c.wantText)
		}
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
