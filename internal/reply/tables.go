package reply

import (
	"strconv"
	"strings"
)

// Slot fragment tables: table name → slot value → spoken text. Each table reads one slot (tableSlot).
var tables = map[string]map[string]string{}

var tableSlot = map[string]string{
	"hour_en": "hour", "hour_ta": "hour", "min_en": "minute", "min_ta": "minute", "minute_en": "minute", "minute_ta": "minute",
	"period_en": "period", "period_ta": "period", "period_rom": "period",
	"day_en": "day", "day_ta": "day", "day_rom": "day", "dayis_en": "day", "dayc_rom": "day",
	"amount_en": "amount", "amount_ta": "amount", "unit_en": "unit", "unit_ta": "unit", "unit_rom": "unit",
	"weekday_en": "weekday", "weekday_ta": "weekday", "month_en": "month", "month_ta": "month", "dom_en": "dom", "dom_ta": "dom",
	"app_en": "app", "app_ta": "app", "app_name": "app",
	"op_en": "op", "a_num": "a", "b_num": "b", "result_num": "result",
}

var (
	taUnits = []string{"", "ஒன்று", "இரண்டு", "மூன்று", "நான்கு", "ஐந்து", "ஆறு", "ஏழு", "எட்டு", "ஒன்பது"}
	taTeens = []string{"பத்து", "பதினொன்று", "பன்னிரண்டு", "பதிமூன்று", "பதினான்கு", "பதினைந்து", "பதினாறு", "பதினேழு", "பதினெட்டு", "பத்தொன்பது"}
	taTens  = []string{"", "", "இருபது", "முப்பது", "நாற்பது", "ஐம்பது"}
	taTensC = []string{"", "", "இருபத்து", "முப்பத்து", "நாற்பத்து", "ஐம்பத்து"}
)

// tamilNumber spells 1–59 in Tamil script.
func tamilNumber(n int) string {
	switch {
	case n <= 0 || n >= 60:
		return ""
	case n < 10:
		return taUnits[n]
	case n < 20:
		return taTeens[n-10]
	case n%10 == 0:
		return taTens[n/10]
	}
	return taTensC[n/10] + " " + taUnits[n%10]
}

func init() {
	set := func(t, k, v string) {
		if tables[t] == nil {
			tables[t] = map[string]string{}
		}
		tables[t][k] = v
	}
	for n := 1; n <= 59; n++ {
		k := strconv.Itoa(n)
		set("minute_en", k, k)
		set("minute_ta", k, tamilNumber(n)+" நிமிஷம்")
		set("amount_en", k, k)
		set("amount_ta", k, tamilNumber(n))
		if n <= 12 {
			set("hour_en", k, k)
			set("hour_ta", k, tamilNumber(n))
		}
		if n <= 31 {
			set("dom_en", k, k)
			set("dom_ta", k, tamilNumber(n))
		}
	}
	set("minute_en", "0", "o'clock")
	set("minute_ta", "0", "")
	// alarm minutes: 0 → nothing, else spoken number
	for n := 1; n <= 59; n++ {
		k := strconv.Itoa(n)
		set("min_en", k, k)
		set("min_ta", k, tamilNumber(n))
	}
	set("min_en", "0", "")
	set("min_ta", "0", "")
	set("min_ta", "30", "அரை")
	set("min_ta", "15", "கால்")
	for k, v := range map[string][3]string{
		"am": {"a.m.", "காலை", "kaalai"}, "noon": {"p.m.", "மதியம்", "madhiyam"},
		"pm": {"p.m.", "சாயங்காலம்", "saayangaalam"}, "night": {"at night", "ராத்திரி", "raathiri"},
	} {
		set("period_en", k, v[0])
		set("period_ta", k, v[1])
		set("period_rom", k, v[2])
	}
	for k, v := range map[string]string{"today": "Today is", "tomorrow": "Tomorrow is", "yesterday": "Yesterday was"} {
		set("dayis_en", k, v)
		set("dayc_rom", k, map[string]string{"today": "Innaikku", "tomorrow": "Naalaikku", "yesterday": "Nethu"}[k])
	}
	for k, v := range map[string][3]string{"today": {"today", "இன்னைக்கு", "innaikku"}, "tomorrow": {"tomorrow", "நாளைக்கு", "naalaikku"}, "yesterday": {"yesterday", "நேத்து", "nethu"}, "": {"", "", ""}} {
		set("day_en", k, v[0])
		set("day_ta", k, v[1])
		set("day_rom", k, v[2])
	}
	for k, v := range map[string][3]string{"minute": {"minutes", "நிமிஷம்", "nimisham"}, "hour": {"hours", "மணி நேரம்", "mani neram"}} {
		set("unit_en", k, v[0])
		set("unit_ta", k, v[1])
		set("unit_rom", k, v[2])
	}
	wd := map[string]string{"Sunday": "ஞாயிறு", "Monday": "திங்கள்", "Tuesday": "செவ்வாய்", "Wednesday": "புதன்", "Thursday": "வியாழன்", "Friday": "வெள்ளி", "Saturday": "சனி"}
	for k, v := range wd {
		set("weekday_en", k, k)
		set("weekday_ta", k, v)
	}
	mo := []string{"ஜனவரி", "பிப்ரவரி", "மார்ச்", "ஏப்ரல்", "மே", "ஜூன்", "ஜூலை", "ஆகஸ்ட்", "செப்டம்பர்", "அக்டோபர்", "நவம்பர்", "டிசம்பர்"}
	for i, m := range []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"} {
		set("month_en", m, m)
		set("month_ta", m, mo[i])
	}
	for n := 0; n <= 200; n++ { // calculator operands/results (larger or decimal results fall back to live TTS)
		k := strconv.Itoa(n)
		set("a_num", k, k)
		set("b_num", k, k)
		set("result_num", k, k)
	}
	for k, v := range map[string][3]string{
		"chrome": {"Chrome", "குரோம்", "Chrome"}, "safari": {"Safari", "சஃபாரி", "Safari"}, "spotify": {"Spotify", "ஸ்பாட்டிஃபை", "Spotify"},
		"music": {"Music", "மியூசிக்", "Music"}, "calculator": {"the Calculator", "கால்குலேட்டர்", "Calculator"}, "notes": {"Notes", "நோட்ஸ்", "Notes"},
		"calendar": {"the Calendar", "காலண்டர்", "Calendar"}, "terminal": {"the Terminal", "டெர்மினல்", "Terminal"}, "finder": {"Finder", "ஃபைண்டர்", "Finder"},
		"photos": {"Photos", "ஃபோட்டோஸ்", "Photos"}, "maps": {"Maps", "மேப்ஸ்", "Maps"}, "mail": {"Mail", "மெயில்", "Mail"},
		"messages": {"Messages", "மெசேஜஸ்", "Messages"}, "whatsapp": {"WhatsApp", "வாட்ஸ்அப்", "WhatsApp"}, "code": {"VS Code", "வி எஸ் கோட்", "VS Code"},
		"settings": {"Settings", "செட்டிங்ஸ்", "Settings"}, "camera": {"the camera", "கேமரா", "Camera"}, "facetime": {"FaceTime", "ஃபேஸ்டைம்", "FaceTime"},
		"slack": {"Slack", "ஸ்லாக்", "Slack"}, "zoom": {"Zoom", "ஜூம்", "Zoom"},
	} {
		set("app_en", k, v[0])
		set("app_ta", k, v[1])
		set("app_name", k, v[2])
	}
	for k, v := range map[string]string{"*": "times", "+": "plus", "-": "minus", "/": "divided by"} {
		set("op_en", k, v)
	}
}

// lookup resolves a table fragment; ok=false if the table or value is unknown.
func lookup(table string, slots map[string]string) (string, bool) {
	t, ok := tables[table]
	if !ok {
		return "", false
	}
	v, ok := t[slots[tableSlot[table]]]
	return strings.TrimSpace(v), ok
}
