package nlu

import "edgevoice/internal/iface"

// StripWake looks for the wake word (role WAKE) within the first few tokens, optionally preceded by
// hey/hi/ok/hello. It returns the utterance with everything up to and including the wake word removed.
// found=false means no wake word; rest is then the input unchanged.
func StripWake(n iface.NormalizedText) (rest iface.NormalizedText, found bool) {
	for i := 0; i < min(len(n.Tags), 3); i++ {
		if n.Tags[i].Role != "WAKE" {
			// only greetings may precede the wake word ("hey/hi/ok computer"), not "my computer ..."
			if n.Tags[i].Role != "GREET" && n.Tokens[i] != "ok" && n.Tokens[i] != "okay" && n.Tokens[i] != "he" && n.Tokens[i] != "hae" {
				break
			}
			continue
		}
		rest = iface.NormalizedText{Tokens: n.Tokens[i+1:], Tags: n.Tags[i+1:]}
		for _, t := range rest.Tags {
			switch t.Lang {
			case "ta":
				rest.TamilHits++
			case "en":
				rest.EnglishHits++
			}
		}
		for j, tok := range rest.Tokens {
			if j > 0 {
				rest.Canonical += " "
			}
			rest.Canonical += tok
		}
		return rest, true
	}
	return n, false
}
