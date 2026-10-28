package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	Country  string    `json:"country"`
	Services []Service `json:"services"`
}

// dataPath returns ~/.config-style path for a file, creating the folder.
func dataPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "aisle7")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func configPath() (string, error) { return dataPath("config.json") }

func LoadConfig() Config {
	cfg := Config{Country: "NL"}
	p, err := configPath()
	if err != nil {
		return cfg
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &cfg)
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg
	}
	if cfg.Country == "" {
		cfg.Country = "NL"
	}
	return cfg
}

func SaveConfig(cfg Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

func (c Config) Has(id int) bool {
	for _, s := range c.Services {
		if s.ID == id {
			return true
		}
	}
	return false
}
