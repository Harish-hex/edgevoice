package nlu

import (
	"bufio"
	"io"
	"strings"

	"edgevoice/internal/iface"
)

// Back-transliteration of English loanwords. The Tamil recognizer writes English words in Tamil script
// ("பிரைம் மினிஸ்டர்" → "piraim minisdar"); for chat we map each unknown token back to a common English
// word with the same voice-neutral consonant skeleton, picking the closest spelling (then most frequent).
// "piraim minisdar yaaru" → "prime minister yaaru". Commands don't need this (the lexicon covers them).

type englishIndex struct {
	byKey map[string][]string // key → words in frequency order
	words map[string]bool
}

// voicelessKey is phoneticKey with voicing and a few Tamil-script artefacts neutralised:
// d→t, b→p, g→k, leading "yu"→"u", and y treated as a vowel.
func voicelessKey(s string) string {
	s = strings.ToLower(s)
	if strings.HasPrefix(s, "yu") {
		s = s[1:]
	}
	s = strings.NewReplacer("d", "t", "b", "p", "g", "k", "z", "s", "y", "a").Replace(s)
	return phoneticKey(s)
}

// LoadEnglish adds a frequency-ordered English word list (one word per line).
func (lx *Lexicon) LoadEnglish(r io.Reader) {
	idx := &englishIndex{byKey: map[string][]string{}, words: map[string]bool{}}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		w := strings.ToLower(strings.TrimSpace(sc.Text()))
		if len(w) < 3 {
			continue
		}
		idx.words[w] = true
		k := voicelessKey(w)
		if len(idx.byKey[k]) < 8 {
			idx.byKey[k] = append(idx.byKey[k], w)
		}
	}
	lx.english = idx
}

// HasEnglish reports whether an English word list is loaded.
func (lx *Lexicon) HasEnglish() bool { return lx.english != nil }

func (lx *Lexicon) englishFor(tok string) (string, bool) {
	if lx.english == nil || len(tok) < 4 {
		return "", false
	}
	if lx.english.words[tok] {
		return tok, true
	}
	// Tamil has one letter (ச) for s/ch/sh: "sennai" → chennai
	if strings.HasPrefix(tok, "s") {
		for _, alt := range []string{"ch" + tok[1:], "sh" + tok[1:]} {
			if lx.english.words[alt] {
				return alt, true
			}
		}
	}
	k := voicelessKey(tok)
	if len(k) < 3 {
		return "", false
	}
	// compare spellings after voicing normalisation (keeps vowels): "feekd"→"feekt" vs "fact"→"fakt"
	norm := func(s string) string {
		return strings.NewReplacer("d", "t", "b", "p", "g", "k", "c", "k", "ph", "f", "ee", "a", "oo", "u").Replace(s)
	}
	best, bestD := "", 1<<30
	for _, w := range lx.english.byKey[k] {
		if d := levenshtein(norm(tok), norm(w)); d < bestD {
			best, bestD = w, d
		}
	}
	return best, best != "" && bestD <= len(tok)/2+1
}

// BackTransliterate replaces unknown tokens of a (romanized Tamil) transcript with English words where a
// confident match exists. Replaced tokens get Role "EN" so they count as understood.
func (lx *Lexicon) BackTransliterate(n iface.NormalizedText) iface.NormalizedText {
	out := n
	out.Tokens = append([]string(nil), n.Tokens...)
	out.Tags = append([]iface.Tag(nil), n.Tags...)
	for i, t := range out.Tags {
		if t.Role != "" || lx.stopwords[out.Tokens[i]] {
			continue
		}
		if w, ok := lx.englishFor(out.Tokens[i]); ok {
			out.Tokens[i] = w
			out.Tags[i] = iface.Tag{Canon: w, Role: "EN", Lang: "en"}
		}
	}
	out.Canonical = strings.Join(out.Tokens, " ")
	return out
}
