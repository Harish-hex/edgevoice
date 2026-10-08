package nlu

import (
	"testing"

	"edgevoice/internal/iface"
)

func norm(s string) iface.NormalizedText {
	return NewNormalizer(MustDefault()).Normalize(iface.Transcript{Text: s})
}

func TestNormalizeCanonical(t *testing.T) {
	cases := []struct{ in, want string }{
		{"naliki morning six ku alarm set pannu", "naalaikku kaalai 6 manikku alarm pannu pannu"},
		{"Set an alarm for 6 o'clock", "pannu an alarm for 6 manikku"},
		{"6ku alarm vei", "6 manikku alarm pannu"},
		{"nali key six o clock alarm pan new", "naalaikku 6 manikku alarm pannu"},
		{"vedhar eppadi", "weather enna"},
		{"alaaram cancel pannu", "alarm cancel pannu"},
		{"forty five minutes timer", "45 nimisham timer"},
		{"naalaiki kaalaila 6 manikku", "naalaikku kaalai 6 manikku"},
	}
	for _, c := range cases {
		if got := norm(c.in).Canonical; got != c.want {
			t.Errorf("Normalize(%q)\n got  %q\n want %q", c.in, got, c.want)
		}
	}
}

func TestLangMode(t *testing.T) {
	cases := map[string]string{
		"naliki morning six ku alarm set pannu": ModeTanglish,
		"set an alarm for 6 o'clock":            ModeEnglish,
		"what time is it":                       ModeEnglish,
		"time enna":                             ModeTanglish,
		"vanakkam":                              ModeTanglish,
	}
	for in, want := range cases {
		if got := LangMode(norm(in)); got != want {
			t.Errorf("LangMode(%q)=%s want %s", in, got, want)
		}
	}
}

func TestTransliterate(t *testing.T) {
	cases := map[string]string{"வெதர்": "vedhar", "அலாரம்": "alaaram", "நாளைக்கு": "naalaikku"}
	for in, want := range cases {
		if got := Transliterate(in); got != want {
			t.Errorf("Transliterate(%q)=%q want %q", in, got, want)
		}
	}
	if got := norm("நாளைக்கு அலாரம்").Canonical; got != "naalaikku alarm" {
		t.Errorf("Tamil script normalize = %q", got)
	}
}
