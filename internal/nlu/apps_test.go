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
		// live session 2026-10-08 22:12 (regression: these were all rejected)
		{"open sportife", "app.open", "Spotify"},
		{"open spartife", "app.open", "Spotify"},
		{"get open spartably", "app.open", "Spotify"},
		{"open spartify", "app.open", "Spotify"},
		{"ஓபன் ஸ்பாட்டிஃபை", "app.open", "Spotify"},
		{"set out a long five minutes", "LLM", ""},
		{"daniel opened spartably", "app.open", "Spotify"},
		{"open spottify and play song", "app.open", "Spotify"},
		{"ஓபன் ஸ்பாட்ஃபை அண்ட் ப்ளே சாங்", "app.open", "Spotify"},
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
