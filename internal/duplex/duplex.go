// Package duplex holds the pure barge-in decision: should the user's speech, heard WHILE the assistant is
// answering, interrupt it? (Full-duplex layer on top of the cascaded pipeline; see docs/PITCH.md.)
package duplex

import "strings"

// Backchannels are listener noises that must not interrupt ("mm-hmm", "ok", "seri", "aamaa").
var backchannels = map[string]bool{
	"mm": true, "mmm": true, "hmm": true, "hm": true, "uh": true, "um": true, "ah": true, "oh": true, "huh": true,
	"ok": true, "okay": true, "yeah": true, "yes": true, "yep": true, "right": true, "sure": true, "fine": true,
	"seri": true, "sari": true, "aama": true, "aamaa": true, "ama": true, "haan": true, "ha": true, "achha": true,
	"mhm": true, "uhhuh": true, "aha": true, "cool": true, "nice": true, "wow": true, "hum": true, "hmm-hmm": true,
	"mmhmm": true, "mm-hmm": true, "uh-huh": true, "hmmm": true, "hum-hum": true, "humm": true, "ahh": true, "ohh": true,
}

// stopWords always interrupt, even when short.
var stopWords = map[string]bool{"stop": true, "wait": true, "no": true, "cancel": true, "enough": true, "quiet": true,
	"niruthu": true, "podhum": true, "venam": true, "pause": true, "hold": true}

var fillers = map[string]bool{"the": true, "a": true, "an": true, "and": true, "is": true, "it": true, "to": true, "of": true}

func words(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,?!'\"-")
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// Decision explains the outcome (for logs / dashboard).
type Decision struct {
	Interrupt bool
	Reason    string // "too-short", "no-words", "backchannel", "echo", "speech"
}

// ShouldInterrupt decides on barge-in from the streaming (English) partial transcript of the user's
// speech so far, how long they have been speaking, and the text the assistant is currently saying.
//   - shorter than minMs, or no recognised words yet → wait
//   - only backchannels ("mm", "ok", "seri") → ignore
//   - mostly the assistant's own words (speaker echo leaking into the mic) → ignore
//   - otherwise → interrupt
func ShouldInterrupt(partial, speaking string, speechMs, minMs int) Decision {
	if speechMs < minMs {
		return Decision{false, "too-short"}
	}
	ws := words(partial)
	if len(ws) == 0 {
		return Decision{false, "no-words"}
	}
	var content []string
	for _, w := range ws {
		if !backchannels[w] && !fillers[w] && len(w) > 1 { // 1-letter fragments ("m m hum") are noise
			content = append(content, w)
		}
	}
	if len(content) == 0 {
		return Decision{false, "backchannel"}
	}
	stop, long := false, false
	for _, w := range content {
		if stopWords[w] {
			stop = true
		}
		if len(w) >= 4 {
			long = true
		}
	}
	if !stop && !long && len(content) < 2 { // a lone short fragment ("mem", "him") is most likely a backchannel
		return Decision{false, "backchannel"}
	}
	if speaking != "" {
		said := map[string]bool{}
		for _, w := range words(speaking) {
			said[w] = true
		}
		hit := 0
		for _, w := range content {
			if said[w] {
				hit++
			}
		}
		if len(content) >= 1 && float64(hit)/float64(len(content)) >= 0.6 {
			return Decision{false, "echo"}
		}
	}
	return Decision{true, "speech"}
}
