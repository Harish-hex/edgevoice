package nlu

import "edgevoice/internal/iface"

// Candidate is one recognizer's view of the utterance (DESIGN: dual ASR, parse-based arbitration).
type Candidate struct {
	Source string // "en" (streaming zipformer) or "ta" (IndicConformer)
	Text   string
	Norm   iface.NormalizedText
	Intent *iface.Intent
}

// Coverage is the fraction of content tokens (non-stopword) the lexicon recognised.
func (lx *Lexicon) Coverage(n iface.NormalizedText) float64 {
	content, known := 0, 0
	for i, t := range n.Tags {
		if lx.stopwords[n.Tokens[i]] {
			continue
		}
		content++
		if t.Role != "" {
			known++
		}
	}
	if content == 0 {
		return 0
	}
	return float64(known) / float64(content)
}

// Choose picks the transcript to act on:
//  1. a candidate that parses into a command beats one that doesn't;
//  2. if both parse: higher parser score, then higher lexicon coverage, then the first (English);
//  3. if neither parses (chat): English, unless the Tamil transcript is clearly Tanglish
//     (coverage ≥ 0.4 and higher than English's).
func (lx *Lexicon) Choose(cs []Candidate) Candidate {
	best := -1
	for i, c := range cs {
		if c.Intent == nil {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		b := cs[best]
		if c.Intent.Score > b.Intent.Score || (c.Intent.Score == b.Intent.Score && lx.Coverage(c.Norm) > lx.Coverage(b.Norm)) {
			best = i
		}
	}
	if best >= 0 {
		return cs[best]
	}
	pick := 0
	for i, c := range cs {
		if c.Source == "ta" && c.Text != "" {
			cov := lx.Coverage(c.Norm)
			if cov >= 0.4 && cov > lx.Coverage(cs[pick].Norm) {
				pick = i
			}
		}
	}
	return cs[pick]
}
