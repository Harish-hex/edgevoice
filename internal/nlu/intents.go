package nlu

import (
	"time"

	"edgevoice/internal/iface"
)

// Parser implements iface.IntentParser with ordered, data-driven rules over tag roles.
type Parser struct {
	Lex       *Lexicon
	Now       func() time.Time // injected for deterministic tests
	Threshold float32          // default 0.5
}

func NewParser(lx *Lexicon) *Parser { return &Parser{Lex: lx, Now: time.Now, Threshold: 0.5} }

type feats struct {
	roles map[string]int
	kw    map[string]bool
	nums  int
	unk   int             // unknown content tokens
	vals  map[string]bool // role:value pairs, e.g. WHO:self
}

func featsOf(n iface.NormalizedText, lx *Lexicon) feats {
	f := feats{roles: map[string]int{}, kw: map[string]bool{}, vals: map[string]bool{}}
	for i, t := range n.Tags {
		if t.Role != "" {
			f.roles[t.Role]++
		}
		if t.Value != "" {
			f.vals[t.Role+":"+t.Value] = true
		}
		if t.Role == "KW" {
			f.kw[t.Value] = true
		}
		if t.Role == "NUM" {
			f.nums++
		}
		if t.Role == "" && !lx.stopwords[n.Tokens[i]] {
			f.unk++
		}
	}
	return f
}

// appSlots returns the app key and its macOS application name (from the lexicon value).
func appSlots(n iface.NormalizedText) map[string]string {
	for _, t := range n.Tags {
		if t.Role == "APP" {
			return map[string]string{"app": t.Canon, "mac_app": t.Value}
		}
	}
	return nil
}

// dayOf returns the first DAY value (today/tomorrow/yesterday), default "today".
func dayOf(n iface.NormalizedText) string {
	for _, t := range n.Tags {
		if t.Role == "DAY" {
			return t.Value
		}
	}
	return "today"
}

// FollowUp resolves short corrections against the previous command, e.g. after "naalaikku enna date",
// "innaikku illa, naalaikku" (not today — tomorrow) re-asks the date for the new day. Returns nil if the
// utterance isn't a day-only follow-up to a day-bearing command.
func FollowUp(prev *iface.Intent, n iface.NormalizedText) *iface.Intent {
	if prev == nil || prev.Name != "clock.date" {
		return nil
	}
	var days []string
	for i, t := range n.Tags {
		switch {
		case t.Role == "DAY":
			days = append(days, t.Value)
		case t.Role == "" && n.Tokens[i] != "illa" && n.Tokens[i] != "not" && n.Tokens[i] != "no":
			// tolerate a couple of filler words, nothing else unknown
		}
	}
	if len(days) == 0 {
		return nil
	}
	slots := map[string]string{"day": days[len(days)-1]} // "innaikku illa naalaikku" -> last day wins
	return &iface.Intent{Name: "clock.date", Slots: slots, Score: 0.9}
}

// Parse returns nil when no rule fires, required slots are missing, or the score is below threshold.
func (p *Parser) Parse(n iface.NormalizedText) *iface.Intent {
	f := featsOf(n, p.Lex)
	now := p.Now()
	has := func(r string) bool { return f.roles[r] > 0 }
	mk := func(name string, slots map[string]string, unkFree bool) *iface.Intent {
		score := float32(1.0)
		if !unkFree && f.unk > 0 {
			// penalty grows with the SHARE of unknown words, so a long command with a few misheard
			// fillers ("…stood out alarm five minutes from now") still parses
			content := f.unk
			for r, k := range f.roles {
				if r != "VOCAB" && r != "EN" {
					content += k
				}
			}
			score -= 0.1*float32(min(f.unk, 2)) + 0.5*float32(f.unk)/float32(content)
		}
		if score < p.Threshold {
			return nil
		}
		if slots == nil {
			slots = map[string]string{}
		}
		return &iface.Intent{Name: name, Slots: slots, Score: score}
	}
	timeSlots := func() (map[string]string, bool) {
		t, ok := extractTime(n.Tags, now)
		if !ok {
			return nil, false
		}
		return map[string]string{"time": t.Format("15:04"), "day": dayWord(t, now)}, true
	}

	// Commands without values (time, date, apps, stop, small talk) are easy to trigger by accident from a
	// long misheard sentence ("…planets are dead…" ≈ date): they may contain at most one unknown word.
	simple := f.unk <= 1
	switch {
	case !simple && (has("APP") || has("DATE") || has("TIME") || has("AT") || has("GREET") || has("WHO") || has("CAN") || (has("NEG") && len(f.kw) == 0)):
		return nil
	case has("APP") && (has("CLOSE") || (has("NEG") && !has("OPEN") && !f.kw["alarm"] && !f.kw["timer"])):
		return mk("app.close", appSlots(n), false)
	case has("APP") && (has("OPEN") || has("DO")):
		return mk("app.open", appSlots(n), false)
	case has("OP") && f.nums >= 2:
		if e, ok := extractExpr(n.Tags); ok {
			return mk("calc", map[string]string{"expression": e}, false)
		}
	case f.kw["alarm"] && has("NEG") && !has("UNIT") && f.nums == 0:
		return mk("alarm.cancel", nil, false)
	case f.kw["alarm"] && f.roles["UNIT"] > 0:
		// "alarm for thirty seconds / five minutes" is a countdown → timer
		if d, ok := extractDuration(n.Tags); ok {
			return mk("timer.set", map[string]string{"duration": fmtDuration(d)}, false)
		}
		return nil
	case f.kw["alarm"]:
		if s, ok := timeSlots(); ok {
			return mk("alarm.set", s, false)
		}
		return nil
	case f.kw["timer"] && has("NEG") && !has("UNIT") && f.nums == 0:
		return mk("timer.cancel", nil, false)
	case f.kw["timer"]:
		if d, ok := extractDuration(n.Tags); ok {
			return mk("timer.set", map[string]string{"duration": fmtDuration(d)}, false)
		}
		return nil
	case f.kw["remind"]:
		s, ok := timeSlots()
		text := freeText(n, p.Lex)
		if !ok || text == "" {
			return nil
		}
		s["text"] = text
		return mk("reminder.set", s, true)
	case has("OFFLINE"):
		return mk("offline.unsupported", nil, true)
	case has("DATE") && (has("WH") || has("DAY")):
		return mk("clock.date", map[string]string{"day": dayOf(n)}, false)
	case (has("TIME") || has("AT")) && has("WH") && f.nums == 0 && f.unk <= 1:
		return mk("clock.time", nil, false)
	case has("CAN") && (has("YOU") || has("WH")) && !has("NUM"):
		return mk("smalltalk.capabilities", nil, true)
	case has("WHO") && (has("YOU") || f.vals["WHO:self"]):
		return mk("smalltalk.identity", nil, false)
	case has("GREET") && len(n.Tokens) <= 3:
		return mk("smalltalk.greet", nil, false)
	case has("NEG") && len(f.kw) == 0 && f.nums == 0 && !has("UNIT"):
		return mk("system.stop", nil, false)
	}
	return nil
}
