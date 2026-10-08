package nlu

import "testing"

func TestAppIntents(t *testing.T) {
	cases := []struct{ in, intent, app string }{
		{"open spotify", "app.open", "Spotify"},
		{"spotify open pannu", "app.open", "Spotify"},
		{"chrome thora", "app.open", "Google Chrome"},
		{"calculator open pannunga", "app.open", "Calculator"},
		{"கால்குலேட்டர் ஓபன் பண்ணு", "app.open", "Calculator"},
		{"ஸ்பாட்டிஃபை திறக்கு", "app.open", "Spotify"},
		{"ஸா்டிபை ஓபன் பண்ணு", "app.open", "Spotify"},
		{"launch vs code", "app.open", "Visual Studio Code"},
		{"close spotify", "app.close", "Spotify"},
		{"spotify moodu", "app.close", "Spotify"},
		{"music podu", "app.open", "Music"},
		{"timer cancel pannu", "timer.cancel", ""},
		{"stop the timer", "timer.cancel", ""},
		{"alarm cancel pannu", "alarm.cancel", ""},
	}
	for _, c := range cases {
		got, slots := parse(c.in)
		if got != c.intent || (c.app != "" && slots["mac_app"] != c.app) {
			t.Errorf("Parse(%q) = %s %v, want %s %s (canon=%q)", c.in, got, slots, c.intent, c.app, norm(c.in).Canonical)
		}
	}
}
