package nlu

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

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
	phrases   [][2][]string // multi-word → single-token rewrites
	english   *englishIndex // optional: common English words for loanword back-transliteration
}

// LoadLexicon parses YAML lexicon bytes; pass nil to use the embedded default.
func LoadLexicon(data []byte) (*Lexicon, error) {
	if data == nil {
		data = lexiconYAML
	}
	var doc struct {
		Stopwords []string          `yaml:"stopwords"`
		Phrases   map[string]string `yaml:"phrases"`
		Vocab     []string          `yaml:"vocab"`
		Entries   []lexEntry        `yaml:"entries"`
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
	for from, to := range doc.Phrases {
		lx.phrases = append(lx.phrases, [2][]string{strings.Fields(from), {to}})
	}
	sort.Slice(lx.phrases, func(i, j int) bool { return len(lx.phrases[i][0]) > len(lx.phrases[j][0]) }) // longest first
	for _, w := range doc.Vocab {
		if _, dup := lx.exact[w]; !dup {
			lx.exact[w] = iface.Tag{Canon: w, Role: "VOCAB", Lang: "ta"}
		}
	}
	return lx, nil
}

// RoleValues lists the distinct values for a role (e.g. APP → macOS app names, the app whitelist).
func (lx *Lexicon) RoleValues(role string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range lx.variants {
		if v.tag.Role == role && v.tag.Value != "" && !seen[v.tag.Value] {
			seen[v.tag.Value] = true
			out = append(out, v.tag.Value)
		}
	}
	return out
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
		if len([]rune(v.form)) < 3 || v.tag.Role == "ABOUT" || v.tag.Role == "OP" || (v.tag.Role == "APP" && len(tok) < 5) {
			continue // never fuzzy-match onto tiny forms like "ku", or onto "pathi" (about)
		}
		d := levenshtein(tok, v.form)
		if d > limit || (v.tag.Role == "NEG" && d > 1) { // "daniel" must not become "cancel"
			continue
		}
		pk := phoneticKey(v.form) == key
		if d < bestD || (d == bestD && pk && !bestPK) {
			best, bestD, bestPK = v.tag, d, pk
		}
	}
	if bestD > limit && len([]rune(tok)) >= 7 {
		// Prefix fallback for glued app/keyword tokens ("spaattipaiva" = spotify + junk).
		for _, v := range lx.variants {
			if (v.tag.Role == "APP" || v.tag.Role == "KW") && len(v.form) >= 6 && strings.HasPrefix(tok, v.form[:len(v.form)-1]) {
				return v.tag, limit, true
			}
		}
	}
	if bestD > limit {
		// Phonetic fallback: same consonant skeleton (≥3 consonants) counts as a near match.
		if len([]rune(tok)) >= 4 && len(key) >= 3 {
			for _, v := range lx.variants {
				if len([]rune(v.form)) >= 4 && phoneticKey(v.form) == key && !exactOnly[v.tag.Role] {
					return v.tag, limit, true
				}
			}
		}
		return iface.Tag{}, 0, false
	}
	return best, bestD, true
}

// exactOnly roles never match by sound alone: operators ("india" ≈ "indu" = into), cancel/stop, "about".
var exactOnly = map[string]bool{"OP": true, "NEG": true, "ABOUT": true}

// d1 is edit distance (app names must be near-exact: "long" must not become "song" = Music).
func d1(a, b string) int { return levenshtein(a, b) }

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
