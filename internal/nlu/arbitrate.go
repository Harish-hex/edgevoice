package nlu

import (
	"math"
	"strings"

	"edgevoice/internal/iface"
)

// Candidate is one recognizer's view of the utterance (DESIGN: dual ASR, parse-based arbitration).
type Candidate struct {
	Source string // "en" (streaming zipformer) or "ta" (IndicConformer)
	Text   string
	Norm   iface.NormalizedText
	Intent *iface.Intent
	// English recognizer confidence (HasConf=false when unavailable, e.g. Tamil CTC or text-only tests):
	// AvgLogProb = mean per-token log-probability; TokensPerWord = BPE fragmentation (garbage splits words).
	HasConf       bool
	AvgLogProb    float64
	TokensPerWord float64
}

// Gate holds the confidence thresholds (calibrated on clean vs garbled transcripts; see docs/PITCH.md).
type Gate struct {
	EnChatMinLP  float64 // English transcript good enough to send to the LLM
	EnCmdMinLP   float64 // English transcript good enough to act on a parsed command (looser: rules are strict)
	EnMaxTPW     float64 // max BPE tokens per word (real words are 1–1.5 tokens; garbage is 2+)
	ChatMinCov   float64 // Tamil transcript: fraction of words understood (lexicon, Tamil vocab, English loanwords)
	ChatMinWords int     // too short to be a real question
}

var DefaultGate = Gate{EnChatMinLP: -0.55, EnCmdMinLP: -1.1, EnMaxTPW: 1.6, ChatMinCov: 0.6, ChatMinWords: 2}

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

func (lx *Lexicon) contentWords(n iface.NormalizedText) int {
	c := 0
	for _, t := range n.Tokens {
		if !lx.stopwords[t] {
			c++
		}
	}
	return c
}

func (g Gate) commandOK(c Candidate) bool {
	return !c.HasConf || (c.AvgLogProb >= g.EnCmdMinLP && c.TokensPerWord <= g.EnMaxTPW+0.6)
}

// chatOK decides whether a transcript is clear enough to hand to the LLM. For Tamil it returns the
// back-transliterated text (English loanwords restored) that the LLM should see.
func (lx *Lexicon) chatOK(g Gate, c Candidate) (iface.NormalizedText, bool) {
	if strings.TrimSpace(c.Text) == "" || lx.contentWords(c.Norm) < g.ChatMinWords {
		return c.Norm, false
	}
	if c.Source == "ta" {
		n := lx.BackTransliterate(c.Norm)
		return n, lx.Coverage(n) >= g.ChatMinCov
	}
	if c.HasConf {
		return c.Norm, c.AvgLogProb >= g.EnChatMinLP && c.TokensPerWord <= g.EnMaxTPW
	}
	return c.Norm, true
}

// Choose picks the transcript to act on, or ok=false when nothing is clear enough ("didn't understand"):
//  1. a confident candidate that parses into a command wins (higher parser score, then lexicon coverage,
//     then English);
//  2. otherwise the first candidate (English, then Tamil) that is clear enough for the LLM;
//  3. otherwise ok=false — fail gracefully instead of answering noise.
func (lx *Lexicon) Choose(cs []Candidate) (Candidate, bool) {
	return lx.ChooseWith(DefaultGate, cs)
}

func (lx *Lexicon) ChooseWith(g Gate, cs []Candidate) (Candidate, bool) {
	best := -1
	for i, c := range cs {
		if c.Intent == nil || !g.commandOK(c) {
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
		return cs[best], true
	}
	for _, c := range cs {
		if n, ok := lx.chatOK(g, c); ok {
			c.Norm, c.Intent = n, nil
			return c, true
		}
	}
	var none Candidate
	if len(cs) > 0 {
		none = cs[0]
	}
	none.Intent = nil
	return none, false
}

// UnclearMode guesses the reply language for a "didn't understand" answer.
func UnclearMode(cs []Candidate) string {
	for _, c := range cs {
		if c.Source == "ta" && c.Norm.TamilHits > 0 {
			for _, e := range cs {
				if e.Source == "en" && e.HasConf && e.AvgLogProb >= DefaultGate.EnChatMinLP {
					return ModeEnglish
				}
			}
			return ModeTanglish
		}
	}
	return ModeEnglish
}

// ConfFromTokens computes the English confidence features from sherpa's per-token log-probs.
func ConfFromTokens(text string, logProbs []float64) (avg, tpw float64) {
	if len(logProbs) == 0 {
		return math.Inf(-1), 99
	}
	s := 0.0
	for _, v := range logProbs {
		s += v
	}
	words := len(strings.Fields(text))
	if words == 0 {
		return s / float64(len(logProbs)), 99
	}
	return s / float64(len(logProbs)), float64(len(logProbs)) / float64(words)
}
