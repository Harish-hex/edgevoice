package nlu

import "testing"

// Tamil-script input as IndicConformer emits it (English loanwords in Tamil script) → intent.
func TestParseTamilScript(t *testing.T) {
	cases := []struct {
		in, intent string
		slots      map[string]string
	}{
		{"நாளைக்கு காலை ஆறு மணிக்கு அலாரம் வை", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"நாளைக்கு மார்னிங் சிக்ஸ் க்கு அலாரம் செட் பண்ணு", "alarm.set", map[string]string{"day": "tomorrow", "time": "06:00"}},
		{"ராத்திரி பத்து மணிக்கு அலாரம் வெச்சிடு", "alarm.set", map[string]string{"time": "22:00"}},
		{"ராத்திரி ஏழு மணிக்கு அலாரம் வெச்சிடு", "alarm.set", map[string]string{"time": "19:00"}},
		{"அஞ்சு நிமிஷம் டைமர் வை", "timer.set", map[string]string{"duration": "5m"}},
		{"எட்டு நிமிஷம் டைமர் வை", "timer.set", map[string]string{"duration": "8m"}},
		{"எய்ட் இன்டு ட்வெல்வ் எவ்ளோ", "calc", map[string]string{"expression": "8*12"}},
		{"டைம் என்ன", "clock.time", nil},
		{"மணி என்ன ஆச்சு", "clock.time", nil},
		{"இன்னைக்கு டேட் என்ன", "clock.date", nil},
		{"வணக்கம்", "smalltalk.greet", nil},
		{"நீ யாரு", "smalltalk.identity", nil},
		{"நாளைக்கு வெதர் எப்படி இருக்கும்", "offline.unsupported", nil},
		{"நிறுத்து", "system.stop", nil},
		{"அலாரம் கேன்சல் பண்ணு", "alarm.cancel", nil},
		{"ஒரு ஜோக் சொல்லு", "LLM", nil},
		{"6 மணிக்கு அலாரம் வை", "alarm.set", map[string]string{"time": "06:00"}},
	}
	for _, c := range cases {
		got, slots := parse(c.in)
		if got != c.intent {
			t.Errorf("Parse(%q) intent=%s want %s (latin=%q canon=%q)", c.in, got, c.intent, Transliterate(c.in), norm(c.in).Canonical)
			continue
		}
		for k, v := range c.slots {
			if slots[k] != v {
				t.Errorf("Parse(%q) slot %s=%q want %q (all=%v)", c.in, k, slots[k], v, slots)
			}
		}
	}
}
