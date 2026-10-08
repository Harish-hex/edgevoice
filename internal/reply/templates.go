// Package reply renders intent results into display text and speech fragments (clip keys).
package reply

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed templates.yaml
var templatesYAML []byte

type Template struct {
	Display string   `yaml:"display"`
	Speech  []string `yaml:"speech"`
	Voice   string   `yaml:"voice"`
}

var templates map[string]map[string]Template

func init() {
	if err := yaml.Unmarshal(templatesYAML, &templates); err != nil {
		panic(fmt.Errorf("reply templates: %w", err))
	}
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// Fragment is one speakable piece: Text plus the voice language ("en" or "ta").
type Fragment struct{ Text, Voice string }

func get(id, mode string) Template {
	if t, ok := templates[id][mode]; ok {
		return t
	}
	return templates["error"][mode]
}

func voiceOf(t Template, mode string) string {
	if t.Voice != "" {
		return t.Voice
	}
	if mode == "TANGLISH" {
		return "ta"
	}
	return "en"
}

// Display renders the on-screen line. {mm} is the zero-padded minute.
func Display(id, mode string, slots map[string]string) string {
	s := withMM(slots)
	out := placeholder.ReplaceAllStringFunc(get(id, mode).Display, func(m string) string {
		k := m[1 : len(m)-1]
		if v, ok := lookup(k, s); ok {
			return v
		}
		return s[k]
	})
	return strings.Join(strings.Fields(out), " ")
}

func withMM(slots map[string]string) map[string]string {
	s := map[string]string{}
	for k, v := range slots {
		s[k] = v
	}
	if m, err := strconv.Atoi(slots["minute"]); err == nil {
		s["mm"] = fmt.Sprintf("%02d", m)
	} else {
		s["mm"] = "00"
	}
	return s
}

// Fragments returns the speech pieces in order; empty table values are skipped.
func Fragments(id, mode string, slots map[string]string) []Fragment {
	t := get(id, mode)
	voice := voiceOf(t, mode)
	var out []Fragment
	for _, f := range t.Speech {
		text := placeholder.ReplaceAllStringFunc(f, func(m string) string {
			k := m[1 : len(m)-1]
			if v, ok := lookup(k, slots); ok {
				return v
			}
			return slots[k]
		})
		if text = strings.TrimSpace(text); text != "" {
			out = append(out, Fragment{text, voice})
		}
	}
	return out
}

// AllFragments lists every pre-synthesizable fragment (static template text + every table value used by
// a template in that voice). Fragments containing free slots ({a}, {text}) are excluded: they go to live TTS.
func AllFragments() []Fragment {
	seen := map[Fragment]bool{}
	for _, modes := range templates {
		for mode, t := range modes {
			voice := voiceOf(t, mode)
			for _, f := range t.Speech {
				ms := placeholder.FindAllStringSubmatch(f, -1)
				if len(ms) == 0 {
					seen[Fragment{f, voice}] = true
					continue
				}
				if len(ms) == 1 && ms[0][0] == f {
					for _, v := range tables[ms[0][1]] {
						if v = strings.TrimSpace(v); v != "" {
							seen[Fragment{v, voice}] = true
						}
					}
				}
			}
		}
	}
	out := make([]Fragment, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Voice+out[i].Text < out[j].Voice+out[j].Text })
	return out
}
