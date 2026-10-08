package nlu

import "testing"

func TestStripWake(t *testing.T) {
	cases := []struct {
		in, rest string
		found    bool
	}{
		{"hey computer what time is it", "enna time is it", true},
		{"computer naalaikku enna date", "naalaikku enna date", true},
		{"ok commuter set a timer for five minutes", "pannu a timer for 5 nimisham", true},
		{"ஹே கம்ப்யூட்டர் டைம் என்ன", "time enna", true},
		{"hey computer", "", true},
		{"hacombewter what time is it", "enna time is it", true},
		{"pay computer", "", true},
		{"hey computer hey computer", "", true},
		{"what time is it", "enna time is it", false},
		{"my computer is slow today", "my computer is slow today", false},
	}
	for _, c := range cases {
		rest, found := StripWake(norm(c.in))
		if found != c.found || (found && rest.Canonical != c.rest) {
			t.Errorf("StripWake(%q) = %q,%v want %q,%v", c.in, rest.Canonical, found, c.rest, c.found)
		}
	}
}
