package nlu

import (
	"testing"
	"time"
)

// Fixed clock: Thu 2026-10-08 20:00 local.
var testNow = time.Date(2026, 10, 8, 20, 0, 0, 0, time.Local)

func parse(s string) (string, map[string]string) {
	lx := MustDefault()
	p := NewParser(lx)
	p.Now = func() time.Time { return testNow }
	in := p.Parse(norm(s))
	if in == nil {
		return "LLM", nil
	}
	return in.Name, in.Slots
}

// PRD §14 seed cases + messy ASR variants (Option B: English ASR mangles Tamil words).
func TestParseSeed(t *testing.T) {
	cases := []struct {
		in, intent string
		slots      map[string]string // subset that must match
	}{
		{"naliki morning six ku alarm set pannu", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"set an alarm for 6 o'clock", "alarm.set", map[string]string{"time": "06:00"}},
		{"naalaikku kaalai aaru manikku alarm vei", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"raathiri pathu manikku alarm vechidu", "alarm.set", map[string]string{"time": "22:00"}},
		{"anju nimisham timer vei", "timer.set", map[string]string{"duration": "5m"}},
		{"set a timer for ten minutes", "timer.set", map[string]string{"duration": "10m"}},
		{"time enna", "clock.time", nil},
		{"what time is it", "clock.time", nil},
		{"mani enna", "clock.time", nil},
		{"alarm cancel pannu", "alarm.cancel", nil},
		{"naalaikku weather eppadi irukkum", "offline.unsupported", nil},
		{"vanakkam", "smalltalk.greet", nil},
		{"nee yaaru", "smalltalk.identity", nil},
		{"who are you", "smalltalk.identity", nil},
		{"tell me a fun fact about space", "LLM", nil},
		{"remind me to call amma at 7", "reminder.set", map[string]string{"time": "07:00", "text": "call amma"}},
		{"ezhu manikku amma ku call panna nyabagapadutthu", "reminder.set", map[string]string{"time": "07:00"}},
		{"what is 12 times 8", "calc", map[string]string{"expression": "12*8"}},
		{"12 into 8 evlo", "calc", map[string]string{"expression": "12*8"}},
		{"innaikku date enna", "clock.date", nil},
		{"what's the date today", "clock.date", nil},
		{"niruthu", "system.stop", nil},
		{"stop", "system.stop", nil},
		// messy ASR renderings
		{"nali key six o clock alarm pan new", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"naalaiki kaalaila 6 manikku alarm vachidu", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"alaaram cancel pannunga", "alarm.cancel", nil},
		{"aaru arai manikku alarm", "alarm.set", map[string]string{"time": "06:30"}},
		{"vedhar eppadi", "offline.unsupported", nil},
		{"wake me up at six tomorrow morning", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
	}
	for _, c := range cases {
		got, slots := parse(c.in)
		if got != c.intent {
			t.Errorf("Parse(%q) intent=%s want %s (canon=%q)", c.in, got, c.intent, norm(c.in).Canonical)
			continue
		}
		for k, v := range c.slots {
			if slots[k] != v {
				t.Errorf("Parse(%q) slot %s=%q want %q (all=%v)", c.in, k, slots[k], v, slots)
			}
		}
	}
}
