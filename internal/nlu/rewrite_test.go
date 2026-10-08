package nlu

import (
	"strings"
	"testing"

	"edgevoice/internal/iface"
)

func iFace(s string) iface.Transcript { return iface.Transcript{Text: s} }

func TestToEnglishQuery(t *testing.T) {
	lx := MustDefault()
	lx.LoadEnglish(strings.NewReader("capital\nprime\nminister\nunited\nstates\npresident\nstate\nindia\ndonald\ntrump\nfact\nchennai\ncricket\nfamous\nwhy\n"))
	cases := map[string]string{
		"இந்தியாவோட கேபட்டல் என்ன அது":         "what is the capital of india?",
		"பிரைம் மினிஸ்டர் யாரு":                "who is prime minister?",
		"டொனால்ட் ட்ரம்ப் யாரு":                "who is donald trump?",
		"சென்னை பத்தி ஒரு ஃபேக்ட் சொல்லு":      "tell me a fact about chennai",
		"யுனைடெட் ஸ்டேட்டோட பிரெசிடென்ட் யாரு": "who is the president of united state?",
	}
	for in, want := range cases {
		n := QueryNormalizer(lx).Normalize(iFace(in))
		if got := lx.ToEnglishQuery(n); got != want {
			t.Errorf("ToEnglishQuery(%q) [%q]\n got  %q\n want %q", in, n.Canonical, got, want)
		}
	}
}
