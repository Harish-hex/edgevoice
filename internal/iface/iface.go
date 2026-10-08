// Package iface holds the component contracts from PRD §6. Implementations are chosen by config.
package iface

import (
	"context"
	"time"
)

type PCM []int16 // 16 kHz mono

const (
	SpeechStart = iota
	SpeechEnd
)

type VADEvent struct {
	Kind int // SpeechStart | SpeechEnd
	At   time.Duration
}
type VAD interface {
	Process(frame PCM) (*VADEvent, error)
}

type Endpointer interface {
	// Update returns true when the user's turn has ended.
	Update(ev *VADEvent, partial string, now time.Duration) bool
}

type Transcript struct {
	Text, Lang string
	Confidence float32
	FinalAt    time.Duration
}
type StreamingASR interface {
	Lang() string
	Accept(pcm PCM)
	Partial() string
	Finalize() Transcript
	Reset()
}

// Tag is the lexicon annotation for one normalized token.
// DECISION(§6): added alongside the PRD fields so the parser can use roles without re-lookup.
type Tag struct {
	Canon string // canonical form, e.g. "naalaikku", "6"
	Role  string // DAY, PERIOD, AT, UNIT, DO, NEG, WH, TIME, NUM, NUMMOD, KW, OP, GREET, WHO, YOU, OFFLINE, DATE, STOP ("" = unknown)
	Value string // role value, e.g. "tomorrow", "am", "minute"
	Lang  string // "ta", "en", or "" for unknown/stopword
}

type NormalizedText struct {
	Tokens      []string
	Canonical   string
	TamilHits   int
	EnglishHits int
	Tags        []Tag // parallel to Tokens
}
type Normalizer interface {
	Normalize(t Transcript) NormalizedText
}

type Intent struct {
	Name  string
	Slots map[string]string
	Score float32
}
type IntentParser interface {
	Parse(n NormalizedText) *Intent
} // nil => LLM path

type Message struct{ Role, Content string }
type LLM interface {
	Stream(ctx context.Context, msgs []Message, maxTokens int) (<-chan string, error)
	Warm(ctx context.Context) error
}

type TTS interface {
	Lang() string
	Synth(text string) (PCM, error)
}

type ClipStore interface {
	Render(templateID string, slots map[string]string, mode string) (PCM, bool)
}
