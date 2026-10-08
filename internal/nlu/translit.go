package nlu

import "strings"

// Tamil script → simple Latin (ITRANS-like). Hand-written table, no ML (PRD §7.4 step 1).
var (
	taVowels = map[rune]string{'அ': "a", 'ஆ': "aa", 'இ': "i", 'ஈ': "ii", 'உ': "u", 'ஊ': "uu", 'எ': "e", 'ஏ': "ee", 'ஐ': "ai", 'ஒ': "o", 'ஓ': "oo", 'ஔ': "au"}
	taCons   = map[rune]string{'க': "k", 'ங': "ng", 'ச': "s", 'ஞ': "nj", 'ட': "d", 'ண': "n", 'த': "dh", 'ந': "n", 'ப': "p", 'ம': "m", 'ய': "y", 'ர': "r", 'ல': "l", 'வ': "v", 'ழ': "zh", 'ள': "l", 'ற': "r", 'ன': "n", 'ஜ': "j", 'ஷ': "sh", 'ஸ': "s", 'ஹ': "h"}
	taSigns  = map[rune]string{'ா': "aa", 'ி': "i", 'ீ': "ii", 'ு': "u", 'ூ': "uu", 'ெ': "e", 'ே': "ee", 'ை': "ai", 'ொ': "o", 'ோ': "oo", 'ௌ': "au"}
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
		if c, ok := taCons[r]; ok {
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
			continue
		}
		if r == 'ஃ' {
			b.WriteString("h")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
