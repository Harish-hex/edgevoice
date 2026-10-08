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
	unk   int // unknown content tokens
}

func featsOf(n iface.NormalizedText, lx *Lexicon) feats {
	f := feats{roles: map[string]int{}, kw: map[string]bool{}}
	for i, t := range n.Tags {
		if t.Role != "" {
			f.roles[t.Role]++
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

// Parse returns nil when no rule fires, required slots are missing, or the score is below threshold.
func (p *Parser) Parse(n iface.NormalizedText) *iface.Intent {
	f := featsOf(n, p.Lex)
	now := p.Now()
	has := func(r string) bool { return f.roles[r] > 0 }
	mk := func(name string, slots map[string]string, unkFree bool) *iface.Intent {
		score := float32(1.0)
		if !unkFree {
			score -= 0.15 * float32(f.unk)
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

	switch {
	case has("OP") && f.nums >= 2:
		if e, ok := extractExpr(n.Tags); ok {
			return mk("calc", map[string]string{"expression": e}, false)
		}
	case f.kw["alarm"] && has("NEG"):
		return mk("alarm.cancel", nil, false)
	case f.kw["alarm"]:
		if s, ok := timeSlots(); ok {
			return mk("alarm.set", s, false)
		}
		return nil
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
		return mk("clock.date", nil, false)
	case (has("TIME") || has("AT")) && has("WH") && f.nums == 0:
		return mk("clock.time", nil, false)
	case has("WHO") && has("YOU"):
		return mk("smalltalk.identity", nil, false)
	case has("GREET") && len(n.Tokens) <= 3:
		return mk("smalltalk.greet", nil, false)
	case has("NEG") && len(f.kw) == 0:
		return mk("system.stop", nil, false)
	}
	return nil
}
