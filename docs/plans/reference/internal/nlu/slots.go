package nlu

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"edgevoice/internal/iface"
)

func num(t iface.Tag) (int, bool) {
	if t.Role != "NUM" {
		return 0, false
	}
	v, err := strconv.Atoi(t.Canon)
	return v, err == nil
}

// extractTime applies PRD §7.4 time rules. Returns the absolute target time.
func extractTime(tags []iface.Tag, now time.Time) (time.Time, bool) {
	hour, minute, hi := -1, 0, -1
	for i, t := range tags {
		v, ok := num(t)
		if !ok || v > 23 {
			continue
		}
		if i+1 < len(tags) && tags[i+1].Role == "UNIT" {
			continue // "5 minutes" is a duration, not a clock time
		}
		hour, hi = v, i
		break
	}
	if hour < 0 {
		return time.Time{}, false
	}
	if hi+1 < len(tags) {
		if m, ok := num(tags[hi+1]); ok && m < 60 {
			minute = m // "6 30"
		}
	}
	period, day := "", ""
	for _, t := range tags {
		switch t.Role {
		case "NUMMOD":
			m, _ := strconv.Atoi(t.Value)
			minute += m
		case "PERIOD":
			if period == "" {
				period = t.Value
			}
		case "DAY":
			if day == "" {
				day = t.Value
			}
		}
	}
	h := hour
	nightEarly := false
	if hour <= 12 {
		switch period {
		case "am":
			if h == 12 {
				h = 0
			}
		case "noon":
			if h <= 6 {
				h += 12
			}
		case "pm":
			if h < 12 {
				h += 12
			}
		case "night":
			switch {
			case h == 12:
				h, nightEarly = 0, true
			case h <= 4:
				nightEarly = true
			default:
				h += 12
			}
		case "":
			if day != "" { // explicit day, no period: 5–11 am, 1–4 pm
				if h >= 1 && h <= 4 {
					h += 12
				}
			}
		}
	}
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	at := func(dayOff, hh int) time.Time { return base.AddDate(0, 0, dayOff).Add(time.Duration(hh)*time.Hour + time.Duration(minute)*time.Minute) }

	switch {
	case day == "tomorrow":
		return at(1, h), true
	case day == "today":
		return at(0, h), true
	case period == "" && hour <= 12:
		// next future occurrence of h or h+12
		cands := []int{h % 12, h%12 + 12}
		for _, off := range []int{0, 1} {
			for _, c := range cands {
				if t := at(off, c); t.After(now) {
					return t, true
				}
			}
		}
	}
	t := at(0, h)
	if nightEarly && now.Hour() >= 12 {
		t = at(1, h)
	}
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t, true
}

// extractDuration: NUM [NUMMOD] UNIT → minutes ("anju nimisham", "arai mani neram").
func extractDuration(tags []iface.Tag) (time.Duration, bool) {
	for i, t := range tags {
		if t.Role != "UNIT" {
			continue
		}
		n, found := 0, false
		if i > 0 {
			if v, ok := num(tags[i-1]); ok {
				n, found = v, true
			} else if tags[i-1].Role == "NUMMOD" {
				// "arai mani neram" = half an hour
				m, _ := strconv.Atoi(tags[i-1].Value)
				return time.Duration(m) * time.Minute, true
			}
		}
		if !found {
			continue
		}
		if t.Value == "hour" {
			return time.Duration(n) * time.Hour, true
		}
		return time.Duration(n) * time.Minute, true
	}
	return 0, false
}

func fmtDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// extractExpr finds NUM OP NUM.
func extractExpr(tags []iface.Tag) (string, bool) {
	for i := 0; i+2 < len(tags); i++ {
		if tags[i].Role == "NUM" && tags[i+1].Role == "OP" && tags[i+2].Role == "NUM" {
			return tags[i].Canon + tags[i+1].Canon + tags[i+2].Canon, true
		}
	}
	return "", false
}

// freeText returns untagged, non-stopword tokens (reminder text).
func freeText(n iface.NormalizedText, lx *Lexicon) string {
	var w []string
	for i, t := range n.Tags {
		if t.Role == "" && !lx.stopwords[n.Tokens[i]] {
			w = append(w, n.Tokens[i])
		}
	}
	return strings.Join(w, " ")
}

func dayWord(t, now time.Time) string {
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return "today"
	}
	return "tomorrow"
}
