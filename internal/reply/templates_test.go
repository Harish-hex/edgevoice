package reply

import (
	"strings"
	"testing"
)

func TestAlarmTanglish(t *testing.T) {
	s := map[string]string{"hour": "6", "minute": "0", "period": "am", "day": "tomorrow"}
	if got := Display("alarm.set", "TANGLISH", s); got != "Seri, naalaikku kaalai 6:00 ku alarm vechiten." {
		t.Errorf("display = %q", got)
	}
	var texts []string
	for _, f := range Fragments("alarm.set", "TANGLISH", s) {
		if f.Voice != "ta" {
			t.Errorf("voice %s", f.Voice)
		}
		texts = append(texts, f.Text)
	}
	if got := strings.Join(texts, "|"); got != "சரி,|நாளைக்கு|காலை|ஆறு|மணிக்கு அலாரம் வெச்சிட்டேன்" {
		t.Errorf("fragments = %q", got)
	}
}

func TestAlarmEnglish(t *testing.T) {
	s := map[string]string{"hour": "6", "minute": "30", "period": "am", "day": "tomorrow"}
	if got := Display("alarm.set", "ENGLISH", s); got != "Done, alarm set for 6:30 a.m. tomorrow." {
		t.Errorf("display = %q", got)
	}
}

func TestEveryIntentHasBothModes(t *testing.T) {
	for _, id := range []string{"alarm.set", "alarm.cancel", "timer.set", "reminder.set", "clock.time", "clock.date", "calc", "system.stop", "smalltalk.greet", "smalltalk.identity", "offline.unsupported", "error", "quick_only"} {
		for _, m := range []string{"ENGLISH", "TANGLISH"} {
			if _, ok := templates[id][m]; !ok {
				t.Errorf("missing template %s/%s", id, m)
			}
		}
	}
}

func TestAllFragmentsResolved(t *testing.T) {
	all := AllFragments()
	if len(all) < 100 {
		t.Errorf("only %d fragments", len(all))
	}
	for _, f := range all {
		if strings.Contains(f.Text, "{") {
			t.Errorf("unresolved fragment %q", f.Text)
		}
	}
}

func TestTamilNumber(t *testing.T) {
	if tamilNumber(6) != "ஆறு" || tamilNumber(45) != "நாற்பத்து ஐந்து" || tamilNumber(20) != "இருபது" {
		t.Error("tamilNumber")
	}
}
