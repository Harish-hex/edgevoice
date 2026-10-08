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
		for t := range tokens {
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

// clean drops markdown symbols TTS would read aloud.
func clean(s string) string {
	s = strings.NewReplacer("*", "", "#", "", "`", "", "_", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
