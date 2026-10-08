package nlu

import "edgevoice/internal/iface"

const (
	ModeEnglish  = "ENGLISH"
	ModeTanglish = "TANGLISH"
)

// LangMode: any Tamil hit → TANGLISH (PRD §7.4, decided by team).
func LangMode(n iface.NormalizedText) string {
	if n.TamilHits >= 1 {
		return ModeTanglish
	}
	return ModeEnglish
}
