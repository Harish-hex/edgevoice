package nlu

import (
	"strings"

	"edgevoice/internal/iface"
)

// ToEnglishQuery rewrites a Tanglish question (romanized, after loanword back-transliteration) into plain
// English for the small LLM, which cannot read romanized Tamil (it echoes it back). Rule-based:
//   - Tamil case endings are stripped from loanwords: "indhiyaavooda" → india + "of" (possessive)
//   - question / function words are translated: enna→what, yaaru→who, pathi→about, sollu→tell me …
//   - possessive and postposition order is flipped: "india-oda capital" → "the capital of india",
//     "chennai pathi oru fact" → "a fact about chennai"
//   - the question word goes first: "… enna adhu" → "what is …?"
//
// Example: "indhiyaavooda keepattal enna adhu" → "what is the capital of india?"
//
// Input should be normalized WITHOUT fuzzy matching (QueryNormalizer): fuzzy matching is tuned for
// commands and would turn free words into command words ("sennai" → "enna").
func (lx *Lexicon) ToEnglishQuery(n iface.NormalizedText) string {
	n = lx.BackTransliterate(n)
	type word struct {
		w    string
		poss bool // "X-oda": X owns the next noun
	}
	var ws []word
	wh, verb := "", ""
	about := -1
	for i, tok := range n.Tokens {
		t := n.Tags[i]
		m, ok := tamilFunction[tok]
		if !ok {
			m, ok = tamilFunction[t.Canon]
		}
		if ok {
			switch m.kind {
			case "wh":
				wh = m.en
			case "verb":
				verb = m.en
			case "about":
				about = len(ws)
			case "word":
				ws = append(ws, word{w: m.en})
			}
			continue
		}
		if t.Role == "NUM" && t.Lang == "ta" && t.Canon == "1" { // "oru" = "a"
			ws = append(ws, word{w: "a"})
			continue
		}
		if t.Role == "ABOUT" {
			about = len(ws)
			continue
		}
		if t.Role != "" && t.Role != "EN" && t.Role != "VOCAB" {
			ws = append(ws, word{w: englishOf(t, tok)})
			continue
		}
		if lx.stopwords[tok] {
			continue
		}
		// strip a possessive/locative ending, then try the English word list on the stem
		w, poss := tok, false
		for _, suf := range []string{"voodu", "vooda", "vodu", "voda", "ooda", "oda", "odu", "oodu"} {
			if stem, ok := strings.CutSuffix(tok, suf); ok && len(stem) >= 3 {
				w, poss = stem, true
				break
			}
		}
		if e, ok := lx.englishFor(w); ok {
			w = e
		} else if t.Role == "EN" {
			w = tok
		}
		ws = append(ws, word{w: w, poss: poss})
	}
	// "X-oda Y" → "the Y of X" (X may be several words: "united states-oda president")
	var phrase []string
	start := 0 // words since the last possessive belong to the owner
	for i := 0; i < len(ws); i++ {
		if ws[i].poss && i+1 < len(ws) {
			owner := append(append([]string{}, phrase[start:]...), ws[i].w)
			phrase = append(phrase[:start], append([]string{"the", ws[i+1].w, "of"}, owner...)...)
			i++
			start = len(phrase)
			continue
		}
		phrase = append(phrase, ws[i].w)
	}
	// "X pathi Y" → "Y about X"
	if about > 0 && about <= len(phrase) {
		phrase = append(append(append([]string{}, phrase[about:]...), "about"), phrase[:about]...)
	}
	body := strings.Join(phrase, " ")
	switch {
	case verb != "":
		return strings.TrimSpace(verb + " " + body)
	case wh != "":
		return strings.TrimSpace(wh+" is "+body) + "?"
	}
	return body
}

type fn struct{ kind, en string }

// tamilFunction maps canonical Tamil function words to English roles.
var tamilFunction = map[string]fn{
	"enna": {"wh", "what"}, "yaaru": {"wh", "who"}, "eppadi": {"wh", "how"}, "epdi": {"wh", "how"},
	"enga": {"wh", "where"}, "eppo": {"wh", "when"}, "yen": {"wh", "why"}, "edhu": {"wh", "which"},
	"ennadhu": {"wh", "what"}, "evlo": {"wh", "how much"}, "evvalavu": {"wh", "how much"},
	"pathi": {"about", ""}, "sollu": {"verb", "tell me"}, "sollunga": {"verb", "tell me"},
	"theriyuma": {"verb", "do you know"}, "adhu": {"skip", ""}, "idhu": {"skip", ""}, "ithu": {"skip", ""},
	"oru": {"word", "a"}, "romba": {"word", "very"}, "nalla": {"word", "good"}, "naan": {"word", "I"},
	"nee": {"word", "you"}, "neenga": {"word", "you"}, "enakku": {"word", "me"}, "irukku": {"skip", ""},
	"irukkum": {"word", "will be"}, "illa": {"word", "not"}, "kathai": {"word", "story"}, "kadhai": {"word", "story"},
	"paattu": {"word", "song"}, "padam": {"word", "movie"}, "saapadu": {"word", "food"}, "ooru": {"word", "town"},
}

// englishOf turns a lexicon tag back into an English word for the LLM.
func englishOf(t iface.Tag, tok string) string {
	switch t.Role {
	case "DAY":
		return map[string]string{"today": "today", "tomorrow": "tomorrow", "yesterday": "yesterday",
			"day_after_tomorrow": "the day after tomorrow", "day_before_yesterday": "the day before yesterday"}[t.Value]
	case "NUM":
		return t.Canon
	case "OFFLINE":
		return t.Canon
	case "APP", "KW":
		return t.Canon
	case "TIME":
		return "time"
	case "DATE":
		return "date"
	case "GREET":
		return "hello"
	}
	if t.Canon != "" {
		return t.Canon
	}
	return tok
}

// QueryNormalizer returns an exact-match normalizer for free questions (see ToEnglishQuery).
func QueryNormalizer(lx *Lexicon) *Normalizer { return &Normalizer{Lex: lx} }
