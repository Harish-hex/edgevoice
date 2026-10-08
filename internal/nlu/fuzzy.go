package nlu

import "strings"

// levenshtein returns the edit distance between a and b (rune-wise).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// phoneticKey is the PRD §7.4 tie-break key: dh/th→t, zh→l, w→v, collapse doubles,
// drop vowels after the first character. Extended for English-ASR spellings of Tamil words:
// c/q/kh/ck→k, sch→sh, ph→f, final y→i (manicu≈manikku, caulay≈kaalai, many≈mani).
func phoneticKey(s string) string {
	s = strings.ToLower(s)
	if strings.HasSuffix(s, "y") {
		s = s[:len(s)-1] + "i"
	}
	s = strings.NewReplacer("sch", "sh", "ck", "k", "kh", "k", "ph", "f", "dh", "t", "th", "t", "zh", "l", "c", "k", "q", "k", "w", "v").Replace(s)
	var b strings.Builder
	var last rune
	for i, r := range s {
		if r == last {
			continue
		}
		last = r
		if i > 0 && strings.ContainsRune("aeiou", r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// maxDist is the fuzzy threshold: ≤2 edits for tokens of 6+ runes, ≤1 for shorter.
// DECISION(§7.4): PRD says ≤2 from 4 runes; that matched "tell"→"ten". Tightened for precision.
func maxDist(tok string) int {
	if len([]rune(tok)) >= 6 {
		return 2
	}
	return 1
}
