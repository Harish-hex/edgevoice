package nlu

import "strings"

// Tamil script → simple Latin (ITRANS-like, colloquial). Hand-written table, no ML (PRD §7.4 step 1).
// Context rules that matter for matching ASR output against the lexicon:
//   - geminates are voiceless: ட்ட→tt, த்த→tt, ப்ப→pp, ச்ச→cch (எட்டு→ettu, பத்து→pattu)
//   - word-initial ட/த→t/th (டைமர்→taimar, தேதி→thedhi), ச→s initially, j after ஞ், else s
var (
	taVowels = map[rune]string{'அ': "a", 'ஆ': "aa", 'இ': "i", 'ஈ': "ii", 'உ': "u", 'ஊ': "uu", 'எ': "e", 'ஏ': "ee", 'ஐ': "ai", 'ஒ': "o", 'ஓ': "oo", 'ஔ': "au"}
	taCons   = map[rune]string{'க': "k", 'ங': "ng", 'ச': "s", 'ஞ': "nj", 'ட': "d", 'ண': "n", 'த': "dh", 'ந': "n", 'ப': "p", 'ம': "m", 'ய': "y", 'ர': "r", 'ல': "l", 'வ': "v", 'ழ': "zh", 'ள': "l", 'ற': "r", 'ன': "n", 'ஜ': "j", 'ஷ': "sh", 'ஸ': "s", 'ஹ': "h"}
	taSigns  = map[rune]string{'ா': "aa", 'ி': "i", 'ீ': "ii", 'ு': "u", 'ூ': "uu", 'ெ': "e", 'ே': "ee", 'ை': "ai", 'ொ': "o", 'ோ': "oo", 'ௌ': "au"}
	geminate = map[rune]string{'ட': "t", 'த': "t", 'ப': "p", 'க': "k", 'ற': "t", 'ச': "c"}
)

const pulli = '்'

// HasTamil reports whether s contains any rune in the Tamil block U+0B80–U+0BFF.
func HasTamil(s string) bool {
	for _, r := range s {
		if r >= 0x0B80 && r <= 0x0BFF {
			return true
		}
	}
	return false
}

func isTamilLetter(r rune) bool { return r >= 0x0B80 && r <= 0x0BFF }

// Transliterate converts Tamil script to lowercase Latin; non-Tamil runes pass through.
func Transliterate(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if v, ok := taVowels[r]; ok {
			b.WriteString(v)
			continue
		}
		c, ok := taCons[r]
		if !ok {
			if r == pulli || taSigns[r] != "" {
				continue // orphan pulli / vowel sign (ASR artefact): drop it
			}
			if r == 'ஃ' {
				b.WriteString("h")
			} else {
				b.WriteRune(r)
			}
			continue
		}
		initial := i == 0 || !isTamilLetter(rs[i-1])
		prevPulliSame := i >= 2 && rs[i-1] == pulli && rs[i-2] == r
		nextGem := i+2 < len(rs) && rs[i+1] == pulli && rs[i+2] == r
		switch {
		case r == 'ஞ' && i+2 < len(rs) && rs[i+1] == pulli && rs[i+2] == 'ச':
			c = "n" // ஞ்ச → nj
		case r == 'ச' && prevPulliSame:
			c = "ch"
		case nextGem && geminate[r] != "":
			c = geminate[r]
		case prevPulliSame && geminate[r] != "":
			c = geminate[r]
		case r == 'ச' && i >= 2 && rs[i-1] == pulli && rs[i-2] == 'ஞ':
			c = "j"
		case initial && r == 'ட':
			c = "t"
		case initial && r == 'த':
			c = "th"
		}
		b.WriteString(c)
		if i+1 < len(rs) {
			if rs[i+1] == pulli {
				i++
				continue
			}
			if sign, ok := taSigns[rs[i+1]]; ok {
				b.WriteString(sign)
				i++
				continue
			}
		}
		b.WriteString("a") // inherent vowel
	}
	return b.String()
}
