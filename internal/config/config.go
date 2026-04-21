package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	mlog "menace/internal/log"
)

type MenaceConfig struct {
	Concurrency   int              `json:"concurrency"`
	MaxRetry      int              `json:"max_retry"`
	ChatCharLimit int              `json:"chat_char_limit"`
	ChatMaxHeight int              `json:"chat_max_height"`
	OllamaBaseURL string           `json:"ollama_base_url"`
	Theme         string           `json:"theme"`
	Keys          KeysConfig       `json:"keys"`
	Indexers      []IndexerConfig  `json:"indexers,omitempty"`
}

// IndexerConfig points to an external indexer binary.
type IndexerConfig struct {
	Binary string `json:"binary"` // path to the indexer binary
}

type KeysConfig struct {
	Normal map[string]string `json:"normal,omitempty"`
	Insert map[string]string `json:"insert,omitempty"`
	Modal  map[string]string `json:"modal,omitempty"`
}

func Default() MenaceConfig {
	return MenaceConfig{
		Concurrency:   3,
		MaxRetry:      2,
		ChatCharLimit: 2000,
		ChatMaxHeight: 20,
		OllamaBaseURL: "http://localhost:11434/v1",
		Theme:         "menace",
	}
}

func Load(menaceDir string) MenaceConfig {
	cfg := Default()
	data, err := os.ReadFile(filepath.Join(menaceDir, "config.json"))
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Log but continue with defaults merged with whatever was parseable
		fmt.Fprintf(os.Stderr, "Warning: config.json parse error: %v\n", err)
	}
	cfg.clamp()
	return cfg
}

// clamp enforces sane bounds on config values.
func (c *MenaceConfig) clamp() {
	if c.Concurrency < 1 {
		c.Concurrency = 1
	} else if c.Concurrency > 20 {
		c.Concurrency = 20
	}
	if c.MaxRetry < 0 {
		c.MaxRetry = 0
	} else if c.MaxRetry > 10 {
		c.MaxRetry = 10
	}
	if c.ChatCharLimit < 100 {
		c.ChatCharLimit = 100
	} else if c.ChatCharLimit > 50000 {
		c.ChatCharLimit = 50000
	}
	if c.ChatMaxHeight < 3 {
		c.ChatMaxHeight = 3
	} else if c.ChatMaxHeight > 100 {
		c.ChatMaxHeight = 100
	}
	if c.OllamaBaseURL != "" {
		if u, err := url.Parse(c.OllamaBaseURL); err != nil || u.Scheme == "" {
			c.OllamaBaseURL = Default().OllamaBaseURL
		}
	}
}

func Save(menaceDir string, cfg MenaceConfig) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		mlog.Error("saveConfig marshal", slog.String("err", err.Error()))
		return
	}
	if err := os.WriteFile(filepath.Join(menaceDir, "config.json"), data, 0644); err != nil {
		mlog.Error("saveConfig write", slog.String("err", err.Error()))
	}
}
