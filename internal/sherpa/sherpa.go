//go:build linux

// Package sherpa wraps sherpa-onnx (CPU execution provider only — never CoreML/CUDA) for VAD, ASR and TTS.
// This is the only cgo package; it builds inside the linux container.
package sherpa

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	so "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"edgevoice/internal/audio"
)

const provider = "cpu" // hard constraint (PRD §2.1)

// ---------- VAD ----------

const VADWindow = 512 // Silero window at 16 kHz = 32 ms

// Hangover: Silero keeps IsSpeech true for MinSilence after speech stops; callers subtract it to get the
// true end of voiced audio (t_last_voiced).
const Hangover = 100 * time.Millisecond

type VAD struct{ v *so.VoiceActivityDetector }

func NewVAD(model string) (*VAD, error) {
	if _, err := os.Stat(model); err != nil {
		return nil, err
	}
	c := so.VadModelConfig{SampleRate: 16000, NumThreads: 1, Provider: provider}
	c.SileroVad = so.SileroVadModelConfig{Model: model, Threshold: 0.5, MinSilenceDuration: float32(Hangover.Seconds()), MinSpeechDuration: 0.1, WindowSize: VADWindow, MaxSpeechDuration: 20}
	return &VAD{so.NewVoiceActivityDetector(&c, 30)}, nil
}

// IsSpeech feeds one 512-sample window and reports the speech state.
func (v *VAD) IsSpeech(win []float32) bool {
	v.v.AcceptWaveform(win)
	for !v.v.IsEmpty() {
		v.v.Pop()
	}
	return v.v.IsSpeech()
}

func (v *VAD) Reset() { v.v.Reset() }

// ---------- Streaming ASR (zipformer transducer) ----------

type StreamingASR struct {
	r *so.OnlineRecognizer
	s *so.OnlineStream
}

func firstMatch(dir, pattern string) string {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

func NewStreamingASR(dir string, threads int, hotwords string, score float32) (*StreamingASR, error) {
	c := so.OnlineRecognizerConfig{}
	c.FeatConfig = so.FeatureConfig{SampleRate: 16000, FeatureDim: 80}
	c.ModelConfig.Transducer = so.OnlineTransducerModelConfig{
		Encoder: firstMatch(dir, "encoder*int8.onnx"), Decoder: firstMatch(dir, "decoder*.onnx"), Joiner: firstMatch(dir, "joiner*int8.onnx")}
	if c.ModelConfig.Transducer.Encoder == "" {
		return nil, fmt.Errorf("asr: no encoder in %s", dir)
	}
	c.ModelConfig.Tokens = filepath.Join(dir, "tokens.txt")
	c.ModelConfig.NumThreads, c.ModelConfig.Provider = threads, provider
	c.DecodingMethod, c.MaxActivePaths = "greedy_search", 4
	if hotwords != "" {
		c.DecodingMethod = "modified_beam_search"
		c.HotwordsFile, c.HotwordsScore = hotwords, score
		c.ModelConfig.ModelingUnit, c.ModelConfig.BpeVocab = "bpe", filepath.Join(dir, "bpe.vocab")
	}
	r := so.NewOnlineRecognizer(&c)
	if r == nil {
		return nil, fmt.Errorf("asr: failed to create recognizer")
	}
	return &StreamingASR{r: r, s: so.NewOnlineStream(r)}, nil
}

func (a *StreamingASR) Accept(f []float32) {
	a.s.AcceptWaveform(16000, f)
	for a.r.IsReady(a.s) {
		a.r.Decode(a.s)
	}
}

func (a *StreamingASR) Partial() string { return strings.ToLower(a.r.GetResult(a.s).Text) }

// Finalize flushes with a little tail padding and returns the final text, then resets.
func (a *StreamingASR) Finalize() string {
	a.s.AcceptWaveform(16000, make([]float32, 16000*3/10))
	a.s.InputFinished()
	for a.r.IsReady(a.s) {
		a.r.Decode(a.s)
	}
	t := strings.ToLower(a.r.GetResult(a.s).Text)
	a.Reset()
	return t
}

func (a *StreamingASR) Reset() {
	so.DeleteOnlineStream(a.s)
	a.s = so.NewOnlineStream(a.r)
}

// ---------- Offline Whisper (baseline) ----------

type Whisper struct{ r *so.OfflineRecognizer }

func NewWhisper(dir string, threads int) (*Whisper, error) {
	c := so.OfflineRecognizerConfig{}
	c.FeatConfig = so.FeatureConfig{SampleRate: 16000, FeatureDim: 80}
	c.ModelConfig.Whisper = so.OfflineWhisperModelConfig{Encoder: firstMatch(dir, "*encoder*.onnx"), Decoder: firstMatch(dir, "*decoder*.onnx"), Language: "en", Task: "transcribe", TailPaddings: -1}
	c.ModelConfig.Tokens = firstMatch(dir, "*tokens.txt")
	c.ModelConfig.NumThreads, c.ModelConfig.Provider = threads, provider
	c.DecodingMethod = "greedy_search"
	r := so.NewOfflineRecognizer(&c)
	if r == nil {
		return nil, fmt.Errorf("whisper: failed to create recognizer from %s", dir)
	}
	return &Whisper{r}, nil
}

func (w *Whisper) Transcribe(f []float32) string {
	if len(f) == 0 {
		return ""
	}
	s := so.NewOfflineStream(w.r)
	defer so.DeleteOfflineStream(s)
	s.AcceptWaveform(16000, f)
	w.r.Decode(s)
	return strings.ToLower(strings.TrimSpace(s.GetResult().Text))
}

// ---------- TTS (VITS: Piper English, MMS Tamil) ----------

type TTS struct {
	t *so.OfflineTts
}

func NewTTS(dir string, threads int) (*TTS, error) {
	c := so.OfflineTtsConfig{MaxNumSentences: 1}
	c.Model.Vits = so.OfflineTtsVitsModelConfig{Model: firstMatch(dir, "*.onnx"), Tokens: filepath.Join(dir, "tokens.txt"), NoiseScale: 0.667, NoiseScaleW: 0.8, LengthScale: 1.0}
	if c.Model.Vits.Model == "" {
		return nil, fmt.Errorf("tts: no model in %s", dir)
	}
	if fi, err := os.Stat(filepath.Join(dir, "espeak-ng-data")); err == nil && fi.IsDir() {
		c.Model.Vits.DataDir = filepath.Join(dir, "espeak-ng-data")
	}
	if _, err := os.Stat(filepath.Join(dir, "lexicon.txt")); err == nil {
		c.Model.Vits.Lexicon = filepath.Join(dir, "lexicon.txt")
	}
	c.Model.NumThreads, c.Model.Provider = threads, provider
	t := so.NewOfflineTts(&c)
	if t == nil {
		return nil, fmt.Errorf("tts: failed to load %s", dir)
	}
	return &TTS{t}, nil
}

// Synth returns 16 kHz int16 PCM.
func (t *TTS) Synth(text string) []int16 { return t.SynthSpeed(text, 1.0) }

// SynthSpeed synthesizes at a speed factor (>1 faster); used for synthetic test data.
func (t *TTS) SynthSpeed(text string, speed float32) []int16 {
	a := t.t.Generate(text, 0, speed)
	if a == nil || len(a.Samples) == 0 {
		return nil
	}
	return audio.ToInt16(audio.Resample(a.Samples, a.SampleRate, audio.SampleRate))
}
