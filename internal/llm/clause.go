package llm

import "strings"

// Clauses turns a token stream into speakable chunks (O6). The FIRST chunk is cut early (≥4 words at a
// word boundary, or any punctuation after ≥2 words) so TTS can start while the LLM keeps generating;
// later chunks split on , . ? ! or ~10 words. With split=false it waits for the full reply (baseline).
func Clauses(tokens <-chan string, split bool) <-chan string {
	out := make(chan string, 8)
	go func() {
		defer close(out)
		var b strings.Builder
		first := true
		flush := func() {
			if s := clean(b.String()); s != "" {
				out <- s
				first = false
			}
			b.Reset()
		}
		var words []string
		for t := range tokens {
			// loop guard: tiny models sometimes repeat a phrase forever ("Tamil Nadu Tamil Nadu …")
			words = append(words, strings.Fields(t)...)
			if looping(words) {
				for range tokens { // drain so the producer can finish
				}
				break
			}
			// a token starting with a space begins a new word: decide on the text *before* it
			if split && strings.HasPrefix(t, " ") {
				words := len(strings.Fields(b.String()))
				if (first && words >= 4) || words >= 10 {
					flush()
				}
			}
			b.WriteString(t)
			if !split {
				continue
			}
			words := len(strings.Fields(b.String()))
			if strings.ContainsAny(t, ".?!") || (strings.Contains(t, ",") && (words >= 3 || first && words >= 2)) {
				flush()
			}
		}
		flush()
	}()
	return out
}

// looping reports whether the text ends in the same 1–4 word phrase repeated 3+ times, or one
// "word" has grown absurdly long (e.g. "100000000…").
func looping(ws []string) bool {
	if n := len(ws); n > 0 && len(ws[n-1]) > 30 {
		return true
	}
	for k := 1; k <= 4; k++ {
		if len(ws) < 3*k {
			continue
		}
		tail := ws[len(ws)-k:]
		rep := true
		for r := 2; r <= 3 && rep; r++ {
			seg := ws[len(ws)-r*k : len(ws)-(r-1)*k]
			for i := range tail {
				if !strings.EqualFold(strings.Trim(seg[i], ".,"), strings.Trim(tail[i], ".,")) {
					rep = false
					break
				}
			}
		}
		if rep {
			return true
		}
	}
	return false
}

// IsHedge reports a chunk that is only an "I'm not sure / I don't know" filler (no information).
func IsHedge(chunk string) bool {
	c := strings.ToLower(strings.Trim(strings.TrimSpace(chunk), ".,!"))
	for _, h := range []string{"i'm not sure", "i’m not sure", "i am not sure", "i am unsure", "i'm unsure", "i don't know", "i do not know",
		"i'm not sure about this information", "i'm not sure if this is accurate", "i am unsure about that question"} {
		if c == h || strings.HasPrefix(c, h+" about") || strings.HasPrefix(c, h+" if") {
			return true
		}
	}
	return false
}

// IsEcho reports whether a reply chunk mostly repeats the user's words: ≥75% of its content words
// (stopwords ignored, small spelling differences tolerated) appear in the question. That is the typical
// failure of a tiny LLM that didn't understand ("Indhiyaavoda capital enna adhu.").
func IsEcho(chunk, question string) bool {
	norm := func(s string) []string {
		var out []string
		for _, w := range strings.Fields(strings.ToLower(s)) {
			w = strings.Trim(w, ".,?!'\"")
			if w != "" && !echoStop[w] {
				out = append(out, w)
			}
		}
		return out
	}
	q, ws := norm(question), norm(chunk)
	if len(ws) < 2 {
		return false
	}
	hit := 0
	for _, w := range ws {
		for _, x := range q {
			if w == x || (len(w) >= 5 && len(x) >= 5 && w[:5] == x[:5]) {
				hit++
				break
			}
		}
	}
	return float64(hit)/float64(len(ws)) >= 0.75
}

var echoStop = map[string]bool{"the": true, "a": true, "an": true, "of": true, "is": true, "are": true, "in": true,
	"to": true, "and": true, "it": true, "that": true, "this": true, "was": true, "for": true, "on": true, "be": true}

// clean drops markdown symbols TTS would read aloud.
func clean(s string) string {
	s = strings.NewReplacer("*", "", "#", "", "`", "", "_", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
