package duplex

import "testing"

func TestShouldInterrupt(t *testing.T) {
	reply := "One fun fact about space is that a day on Venus is longer than its year."
	cases := []struct {
		name, partial, speaking string
		ms                      int
		want                    bool
		reason                  string
	}{
		{"too short", "stop", reply, 200, false, "too-short"},
		{"no words yet", "", reply, 600, false, "no-words"},
		{"backchannel en", "mm hmm", reply, 600, false, "backchannel"},
		{"backchannel ta", "seri seri", reply, 600, false, "backchannel"},
		{"ok yeah", "ok yeah", reply, 800, false, "backchannel"},
		{"mm hmm as heard by ASR", "m m hum", reply, 600, false, "backchannel"},
		{"lone short fragment", "mem", reply, 600, false, "backchannel"},
		{"short stop word", "no", reply, 400, true, "speech"},
		{"echo of own voice", "a day on venus", reply, 700, false, "echo"},
		{"real interruption", "stop", reply, 400, true, "speech"},
		{"new question", "what time is it", reply, 500, true, "speech"},
		{"tanglish via english asr", "now lay coom date", reply, 500, true, "speech"},
		{"interrupt while silent reply", "cancel", "", 400, true, "speech"},
	}
	for _, c := range cases {
		d := ShouldInterrupt(c.partial, c.speaking, c.ms, 350)
		if d.Interrupt != c.want || d.Reason != c.reason {
			t.Errorf("%s: got %+v want %v/%s", c.name, d, c.want, c.reason)
		}
	}
}
