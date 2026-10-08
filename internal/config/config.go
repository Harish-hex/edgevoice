// Package config loads YAML run configs. Files may `extends:` another file (ablations/tiers override defaults).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Name     string `yaml:"name"`
	ModelDir string `yaml:"model_dir"`
	Models   struct {
		VAD        string `yaml:"vad"`
		ASRDir     string `yaml:"asr_dir"`
		WhisperDir string `yaml:"whisper_dir"`
		TTSEn      string `yaml:"tts_en"`
		TTSTa      string `yaml:"tts_ta"`
		LLM        string `yaml:"llm"`
	} `yaml:"models"`
	ASR struct {
		Engine   string  `yaml:"engine"` // zipformer | whisper
		Hotwords bool    `yaml:"hotwords"`
		Score    float32 `yaml:"hotwords_score"`
	} `yaml:"asr"`
	Endpoint struct {
		SilenceMs      int `yaml:"silence_ms"`
		EarlySilenceMs int `yaml:"early_silence_ms"` // 0 disables semantic endpointing (O3)
		MaxUtteranceMs int `yaml:"max_utterance_ms"`
	} `yaml:"endpoint"`
	NLU struct {
		FastPath bool `yaml:"fast_path"` // O7: false => everything goes to the LLM
		Fuzzy    bool `yaml:"fuzzy"`
		Merge    bool `yaml:"merge"`
	} `yaml:"nlu"`
	Reply struct {
		Clips    bool   `yaml:"clips"` // O8
		ClipsDir string `yaml:"clips_dir"`
	} `yaml:"reply"`
	LLM struct {
		Enabled     bool `yaml:"enabled"`
		Threads     int  `yaml:"threads"`
		Ctx         int  `yaml:"ctx"`
		MaxTokens   int  `yaml:"max_tokens"`
		PromptCache bool `yaml:"prompt_cache"` // O4
		ClauseTTS   bool `yaml:"clause_tts"`   // O6: false => wait for full reply
		TimeoutMs   int  `yaml:"timeout_ms"`
		Port        int  `yaml:"port"`
	} `yaml:"llm"`
	Degrade bool   `yaml:"degrade"`
	Results string `yaml:"results_dir"`
}

// Load reads path, recursively applying `extends:` (relative to the file) first.
func Load(path string) (*Config, error) {
	c := &Config{}
	if err := load(path, c, 0); err != nil {
		return nil, err
	}
	return c, nil
}

func load(path string, c *Config, depth int) error {
	if depth > 5 {
		return fmt.Errorf("config: extends too deep at %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var head struct {
		Extends string `yaml:"extends"`
	}
	if err := yaml.Unmarshal(b, &head); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if head.Extends != "" {
		if err := load(filepath.Join(filepath.Dir(path), head.Extends), c, depth+1); err != nil {
			return err
		}
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// P joins a model-relative path with ModelDir.
func (c *Config) P(rel string) string { return filepath.Join(c.ModelDir, rel) }
