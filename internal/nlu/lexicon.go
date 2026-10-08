package nlu

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"

	"edgevoice/internal/iface"
)

//go:embed lexicon.yaml
var lexiconYAML []byte

type lexEntry struct {
	Canon string   `yaml:"canon"`
	Role  string   `yaml:"role"`
	Value string   `yaml:"value"`
	TA    []string `yaml:"ta"`
	EN    []string `yaml:"en"`
}

type variant struct {
	form string
	tag  iface.Tag
}

// Lexicon maps heard forms to tags; exact lookup plus a variant list for fuzzy scans (~300 forms).
type Lexicon struct {
	exact     map[string]iface.Tag
	variants  []variant
	stopwords map[string]bool
}

// LoadLexicon parses YAML lexicon bytes; pass nil to use the embedded default.
func LoadLexicon(data []byte) (*Lexicon, error) {
	if data == nil {
		data = lexiconYAML
	}
	var doc struct {
		Stopwords []string   `yaml:"stopwords"`
		Entries   []lexEntry `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("lexicon: %w", err)
	}
	lx := &Lexicon{exact: map[string]iface.Tag{}, stopwords: map[string]bool{}}
	for _, s := range doc.Stopwords {
		lx.stopwords[s] = true
	}
	add := func(e lexEntry, forms []string, lang string) {
		for _, f := range forms {
			tag := iface.Tag{Canon: e.Canon, Role: e.Role, Value: e.Value, Lang: lang}
			if _, dup := lx.exact[f]; !dup {
				lx.exact[f] = tag
			}
			lx.variants = append(lx.variants, variant{f, tag})
		}
	}
	for _, e := range doc.Entries {
		add(e, e.TA, "ta")
		add(e, e.EN, "en")
	}
	return lx, nil
}

// MustDefault returns the embedded lexicon or panics (embedded data is covered by tests).
func MustDefault() *Lexicon {
	lx, err := LoadLexicon(nil)
	if err != nil {
		panic(err)
	}
	return lx
}

// lookup returns (tag, distance, ok). Exact → 0. Fuzzy only for tokens of 3+ runes; ties broken by
// phonetic-key equality.
func (lx *Lexicon) lookup(tok string) (iface.Tag, int, bool) {
	if t, ok := lx.exact[tok]; ok {
		return t, 0, true
	}
	if len([]rune(tok)) < 4 || isDigits(tok) { // 1–3 letter tokens: exact match only
		return iface.Tag{}, 0, false
	}
	limit := maxDist(tok)
	key := phoneticKey(tok)
	best, bestD, bestPK := iface.Tag{}, limit+1, false
	for _, v := range lx.variants {
		if len([]rune(v.form)) < 3 {
			continue // never fuzzy-match onto tiny forms like "ku"
		}
		d := levenshtein(tok, v.form)
		if d > limit {
			continue
		}
		pk := phoneticKey(v.form) == key
		if d < bestD || (d == bestD && pk && !bestPK) {
			best, bestD, bestPK = v.tag, d, pk
		}
	}
	if bestD > limit {
		// Phonetic fallback: same consonant skeleton (≥3 consonants) counts as a near match.
		if len([]rune(tok)) >= 4 && len(key) >= 3 {
			for _, v := range lx.variants {
				if len([]rune(v.form)) >= 4 && phoneticKey(v.form) == key {
					return v.tag, limit, true
				}
			}
		}
		return iface.Tag{}, 0, false
	}
	return best, bestD, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
