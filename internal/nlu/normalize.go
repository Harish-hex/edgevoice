package nlu

import (
	"strconv"
	"strings"
	"unicode"

	"edgevoice/internal/iface"
)

// Normalizer implements iface.Normalizer (PRD §7.4 steps 1–5 plus adjacent-token merge, DESIGN #7).
type Normalizer struct {
	Lex   *Lexicon
	Fuzzy bool // ablation flag: false = exact lookup only
	Merge bool // ablation flag: merge adjacent unknown tokens
}

func NewNormalizer(lx *Lexicon) *Normalizer { return &Normalizer{Lex: lx, Fuzzy: true, Merge: true} }

func (n *Normalizer) Normalize(t iface.Transcript) iface.NormalizedText {
	text := t.Text
	if HasTamil(text) {
		text = Transliterate(text)
	}
	raw := n.applyPhrases(n.splitKeywordPrefixes(tokenize(text)))

	var toks []string
	var tags []iface.Tag
	for i := 0; i < len(raw); i++ {
		tok := raw[i]
		if n.Lex.stopwords[tok] {
			toks, tags = append(toks, tok), append(tags, iface.Tag{Canon: tok})
			continue
		}
		tag, d, ok := n.find(tok)
		// Merge: "nali key" → "nalikey" ≈ naliki; only when neither half is an exact hit.
		if n.Merge && i+1 < len(raw) && (!ok || d > 0) && !n.Lex.stopwords[raw[i+1]] {
			if _, d2, ok2 := n.find(raw[i+1]); !(ok2 && d2 == 0) {
				if mt, md, mok := n.find(tok + raw[i+1]); mok && (!ok || md <= d) {
					toks, tags = append(toks, mt.Canon), append(tags, mt)
					i++
					continue
				}
			}
		}
		if ok {
			toks, tags = append(toks, tag.Canon), append(tags, tag)
			continue
		}
		if isDigits(tok) {
			toks, tags = append(toks, tok), append(tags, iface.Tag{Canon: tok, Role: "NUM"})
			continue
		}
		toks, tags = append(toks, tok), append(tags, iface.Tag{Canon: tok})
	}
	toks, tags = combineTens(toks, tags)
	// Tamil-script English numbers next to an operator: "on plas on" = 1 + 1 ("on" is otherwise a stopword)
	for i := range toks {
		if toks[i] == "on" && ((i > 0 && tags[i-1].Role == "OP") || (i+1 < len(tags) && tags[i+1].Role == "OP")) {
			toks[i], tags[i] = "1", iface.Tag{Canon: "1", Role: "NUM", Lang: "ta"}
		}
	}

	out := iface.NormalizedText{Tokens: toks, Tags: tags, Canonical: strings.Join(toks, " ")}
	for _, tg := range tags {
		switch tg.Lang {
		case "ta":
			out.TamilHits++
		case "en":
			out.EnglishHits++
		}
	}
	return out
}

// applyPhrases rewrites known multi-word phrases into single tokens (longest match first).
func (n *Normalizer) applyPhrases(toks []string) []string {
	var out []string
	for i := 0; i < len(toks); {
		matched := false
		for _, ph := range n.Lex.phrases {
			from := ph[0]
			if i+len(from) <= len(toks) && strings.Join(toks[i:i+len(from)], " ") == strings.Join(from, " ") {
				out = append(out, ph[1][0])
				i += len(from)
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, toks[i])
			i++
		}
	}
	return out
}

// splitKeywordPrefixes splits ASR-glued tokens that start with an intent keyword ("timervey" → "timer vey").
func (n *Normalizer) splitKeywordPrefixes(toks []string) []string {
	var out []string
	for _, t := range toks {
		if _, ok := n.Lex.exact[t]; !ok && len(t) >= 7 {
			split := false
			for _, kw := range []string{"alarm", "timer"} {
				if rest, ok := strings.CutPrefix(t, kw); ok && len(rest) >= 2 {
					out = append(out, kw, rest)
					split = true
					break
				}
			}
			if split {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func (n *Normalizer) find(tok string) (iface.Tag, int, bool) {
	if !n.Fuzzy {
		t, ok := n.Lex.exact[tok]
		return t, 0, ok
	}
	return n.Lex.lookup(tok)
}

// tokenize lowercases, turns punctuation into spaces (apostrophes vanish: o'clock → oclock),
// and splits digit runs from letter suffixes (6ku → 6 ku).
func tokenize(s string) []string {
	var b strings.Builder
	var prev rune
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r):
			if prev != 0 && unicode.IsDigit(prev) != unicode.IsDigit(r) && (unicode.IsDigit(prev) || unicode.IsLetter(prev)) {
				b.WriteRune(' ')
			}
			b.WriteRune(r)
			prev = r
		default:
			b.WriteRune(' ')
			prev = 0
		}
	}
	return strings.Fields(b.String())
}

// combineTens merges "20 5" → "25" (forty five, irupathu anju).
func combineTens(toks []string, tags []iface.Tag) ([]string, []iface.Tag) {
	var ot []string
	var og []iface.Tag
	for i := 0; i < len(toks); i++ {
		if tags[i].Role == "NUM" && i+1 < len(toks) && tags[i+1].Role == "NUM" {
			a, _ := strconv.Atoi(toks[i])
			b, _ := strconv.Atoi(toks[i+1])
			if a >= 20 && a%10 == 0 && b >= 1 && b <= 9 {
				c := strconv.Itoa(a + b)
				tg := tags[i]
				tg.Canon = c
				ot, og = append(ot, c), append(og, tg)
				i++
				continue
			}
		}
		ot, og = append(ot, toks[i]), append(og, tags[i])
	}
	return ot, og
}
