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
		{"innaikku date enna", "clock.date", map[string]string{"day": "today"}},
		{"naalaikku enna date", "clock.date", map[string]string{"day": "tomorrow"}},
		{"what's the date tomorrow", "clock.date", map[string]string{"day": "tomorrow"}},
		{"nethu enna date", "clock.date", map[string]string{"day": "yesterday"}},
		{"what's the date today", "clock.date", nil},
		{"niruthu", "system.stop", nil},
		{"stop", "system.stop", nil},
		// messy ASR renderings
		{"nali key six o clock alarm pan new", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"naalaiki kaalaila 6 manikku alarm vachidu", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"alaaram cancel pannunga", "alarm.cancel", nil},
		{"aaru arai manikku alarm", "alarm.set", map[string]string{"time": "06:30"}},
		{"vedhar eppadi", "offline.unsupported", nil},
		// English-ASR renderings of romanized Tanglish (from synthetic dev set)
		{"now lay coom mourning six coup alarm set panu", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"now lay cou call i pathoo manicu alarm vay", "alarm.set", map[string]string{"day": "tomorrow", "time": "10:00"}},
		{"onu nimischem timer fa", "timer.set", map[string]string{"duration": "1m"}},
		{"niaru", "smalltalk.identity", nil},
		// live session 2026-10-08 21:41
		{"arguess stood out alarm five minutes from now", "timer.set", map[string]string{"duration": "5m"}},
		{"set an alarm five minutes from now", "timer.set", map[string]string{"duration": "5m"}},
		{"what can you do", "smalltalk.capabilities", nil},
		{"உன்னால என்னாலாம் பண்ண முடியும்", "smalltalk.capabilities", nil},
		{"உன்னால என்னாலா பண்ண முடியும்", "smalltalk.capabilities", nil},
		{"how many planets are there in the solar system", "LLM", nil},
		{"how many planets are dead in the solar system", "LLM", nil},
		{"argus set out a long five minutes from now", "LLM", nil},
		{"daniel settin alone five minutes from now", "LLM", nil},
		{"daniel set in alarm five minutes from now", "timer.set", map[string]string{"duration": "5m"}},
		{"or ester alarm for so in o'clock to morrow", "LLM", nil},
		{"set a timer for thirty seconds", "timer.set", map[string]string{"duration": "30s"}},
		{"set an alarm for thirty seconds", "timer.set", map[string]string{"duration": "30s"}},
		{"naalanniki enna date", "clock.date", map[string]string{"day": "day_after_tomorrow"}},
		{"what's the date day after tomorrow", "clock.date", map[string]string{"day": "day_after_tomorrow"}},
		{"munthaanethu enna date", "clock.date", map[string]string{"day": "day_before_yesterday"}},
		{"innaikku enna kizhamai", "clock.date", map[string]string{"day": "today"}},
		{"what day is it today", "clock.date", map[string]string{"day": "today"}},
		{"onu neme timervey", "timer.set", map[string]string{"duration": "1m"}},
		{"mani enna aachu", "clock.time", nil},
		{"six into ten avla", "calc", map[string]string{"expression": "6*10"}},
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
