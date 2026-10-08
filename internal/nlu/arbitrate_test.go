package nlu

import (
	"testing"
	"time"

	"edgevoice/internal/iface"
)

func cand(src, text string) Candidate {
	lx := MustDefault()
	p := NewParser(lx)
	p.Now = func() time.Time { return testNow }
	n := NewNormalizer(lx).Normalize(iface.Transcript{Text: text})
	return Candidate{Source: src, Text: text, Norm: n, Intent: p.Parse(n)}
}

func TestChoose(t *testing.T) {
	lx := MustDefault()
	cases := []struct {
		en, ta, wantSrc, wantIntent string
	}{
		// Tanglish: English ASR produces garbage, Tamil ASR parses
		{"not like you call a lot of money colorambi", "நாளைக்கு காலை ஆறு மணிக்கு அலாரம் வை", "ta", "alarm.set"},
		// English: both may parse; English wins ties
		{"what time is it", "வாட் டைம் இஸ் இட்", "en", "clock.time"},
		{"set a timer for five minutes", "செட் எ டைமர் ஃபார் ஃபைவ் மினிட்ஸ்", "en", "timer.set"},
		// English chat: neither parses -> English text goes to the LLM
		{"tell me a fun fact about space", "டெல் மீ எ ஃபன் ஃபேக்ட் அபவுட் ஸ்பேஸ்", "en", ""},
		// Tanglish chat: Tamil transcript clearly Tanglish
		{"or a joke so", "நாளைக்கு என்ன பண்ணலாம்", "ta", ""},
	}
	for _, c := range cases {
		got := lx.Choose([]Candidate{cand("en", c.en), cand("ta", c.ta)})
		intent := ""
		if got.Intent != nil {
			intent = got.Intent.Name
		}
		if got.Source != c.wantSrc || intent != c.wantIntent {
			t.Errorf("Choose(%q | %q) = %s/%q want %s/%q (ta canon=%q cov=%.2f)", c.en, c.ta, got.Source, intent, c.wantSrc, c.wantIntent,
				cand("ta", c.ta).Norm.Canonical, lx.Coverage(cand("ta", c.ta).Norm))
		}
	}
}
