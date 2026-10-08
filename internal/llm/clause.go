package llm

import "strings"

// Clauses turns a token stream into speakable clauses: split on , . ? ! (or ~8 words) (O6).
// With split=false it waits for the full reply (baseline behaviour).
func Clauses(tokens <-chan string, split bool) <-chan string {
	out := make(chan string, 8)
	go func() {
		defer close(out)
		var b strings.Builder
		flush := func() {
			if s := clean(b.String()); s != "" {
				out <- s
			}
			b.Reset()
		}
		for t := range tokens {
			b.WriteString(t)
			if !split {
				continue
			}
			s := b.String()
			if strings.ContainsAny(t, ".?!") || (strings.Contains(t, ",") && len(strings.Fields(s)) >= 3) || len(strings.Fields(s)) >= 8 && strings.HasSuffix(s, " ") {
				flush()
			}
		}
		flush()
	}()
	return out
}

// clean drops markdown/emoji-ish symbols TTS would read aloud.
func clean(s string) string {
	s = strings.NewReplacer("*", "", "#", "", "`", "", "_", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
