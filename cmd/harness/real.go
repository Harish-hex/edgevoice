package main

import (
	"fmt"
	"strings"
)

// outcome maps a turn record to what the system did: an intent name, "llm", "unclear" (said it didn't
// understand / stayed silent), or "wake".
func outcome(m map[string]any) string {
	switch m["route"] {
	case "cmd":
		return fmt.Sprint(m["intent"])
	case "llm":
		return "llm"
	case "wake":
		return "wake"
	}
	return "unclear" // unclear, empty, asleep
}

// realScore scores one run against labels with alternatives ("app.open|unclear") and classes.
type realScore struct {
	n, cmdOK, cmdN, qOK, qN, noiseOK, noiseN, wakeOK, wakeN, wrongAction, nonsense int
	rows                                                                           []string
}

func scoreReal(lab map[string]label, recs []map[string]any) realScore {
	var s realScore
	seen := map[string]bool{}
	for _, m := range recs {
		file := fmt.Sprint(m["file"])
		l, ok := lab[file]
		if !ok || seen[file] {
			continue
		}
		seen[file] = true
		s.n++
		got := outcome(m)
		alts := strings.Split(l.Intent, "|")
		hit := false
		for _, a := range alts {
			if a == got {
				hit = true
			}
		}
		if hit && got != "unclear" && got != "llm" && got != "wake" {
			gs, _ := m["slots"].(map[string]any)
			for k, v := range l.Slots {
				if fmt.Sprint(gs[k]) != v {
					hit = false
				}
			}
		}
		primary := alts[0]
		switch primary {
		case "unclear":
			s.noiseN++
			if hit {
				s.noiseOK++
			}
		case "llm":
			s.qN++
			if hit {
				s.qOK++
			}
		case "wake":
			s.wakeN++
			if hit {
				s.wakeOK++
			}
		default:
			s.cmdN++
			if hit {
				s.cmdOK++
			}
		}
		if !hit && got != "unclear" && got != "llm" && got != "wake" {
			s.wrongAction++ // executed a command the user didn't ask for
		}
		if !hit && got == "llm" {
			s.nonsense++ // chatted about something that wasn't a clear question
		}
		mark := "✓"
		if !hit {
			mark = "✗"
		}
		heard := fmt.Sprint(m["transcript"])
		if len([]rune(heard)) > 48 {
			heard = string([]rune(heard)[:48]) + "…"
		}
		s.rows = append(s.rows, fmt.Sprintf("%s %-12s gold=%-18s got=%-18s heard=%q", mark, file, l.Intent, got, heard))
	}
	return s
}

func pctS(a, b int) string {
	if b == 0 {
		return "–"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(a)/float64(b), a, b)
}
