package nlu

import (
	"regexp"

	"edgevoice/internal/iface"
)

// wakeLike catches mangled "computer" (e.g. "hacombewter", "kompyuter"): k-m-p/b-...-t-r skeleton.
var wakeLike = regexp.MustCompile(`k[mn][pb][a-z]{0,3}t[a-z]{0,2}r?$`)

// wakeFiller: sounds allowed before the wake word ("hey"/"ok" as heard by either recognizer).
var wakeFiller = map[string]bool{"ok": true, "okay": true, "he": true, "hae": true, "hee": true, "hei": true, "haai": true, "heey": true, "pay": true, "a": true, "hay": true}

func isWake(tok string, t iface.Tag) bool {
	return t.Role == "WAKE" || (len(tok) >= 6 && wakeLike.MatchString(phoneticKey(tok)))
}

// StripWake looks for the wake word (role WAKE) within the first few tokens, optionally preceded by
// hey/hi/ok/hello. It returns the utterance with everything up to and including the wake word removed.
// found=false means no wake word; rest is then the input unchanged.
func StripWake(n iface.NormalizedText) (rest iface.NormalizedText, found bool) {
	for i := 0; i < min(len(n.Tags), 3); i++ {
		if !isWake(n.Tokens[i], n.Tags[i]) {
			// only greetings may precede the wake word ("hey/hi/ok computer"), not "my computer ..."
			if n.Tags[i].Role != "GREET" && !wakeFiller[n.Tokens[i]] {
				break
			}
			continue
		}
		rest = iface.NormalizedText{Tokens: n.Tokens[i+1:], Tags: n.Tags[i+1:]}
		if r2, again := StripWake(rest); again { // "hey computer, hey computer …"
			return r2, true
		}
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
