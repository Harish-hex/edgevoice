package pipeline

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"edgevoice/internal/audio"
	"edgevoice/internal/reply"
)

// ClipStore holds pre-synthesized fragments in memory (O8) and stitches them (PRD §7.5).
type ClipStore struct{ clips map[string][]int16 }

func ClipKey(f reply.Fragment) string {
	h := sha1.Sum([]byte(f.Voice + "|" + f.Text))
	return hex.EncodeToString(h[:8])
}

// LoadClips reads dir/manifest.json ({key: file}) and all referenced raw PCM files.
func LoadClips(dir string) (*ClipStore, error) {
	cs := &ClipStore{clips: map[string][]int16{}}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return cs, err
	}
	var man map[string]string
	if err := json.Unmarshal(b, &man); err != nil {
		return cs, err
	}
	for k, f := range man {
		if raw, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			cs.clips[k] = audio.BytesToPCM(raw)
		}
	}
	return cs, nil
}

func (cs *ClipStore) Len() int { return len(cs.clips) }

// Render stitches fragments with a 40 ms crossfade; ok=false if any fragment is missing.
func (cs *ClipStore) Render(frags []reply.Fragment) ([]int16, bool) {
	var parts [][]int16
	for _, f := range frags {
		c, ok := cs.clips[ClipKey(f)]
		if !ok {
			return nil, false
		}
		parts = append(parts, c)
	}
	return audio.Join(parts, 40), true
}
