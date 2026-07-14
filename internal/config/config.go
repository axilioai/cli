// Package config stores CLI credentials in a language-agnostic file that every
// axilio SDK also reads, so a single `axilio login` makes the CLI and the SDKs
// work. Path: $XDG_CONFIG_HOME/axilio/config.json (else ~/.config/...), mode 0600.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the on-disk credential file. Field names match what the SDKs read;
// ActiveOrg is CLI-only (the SDKs ignore unknown keys) and records the org an
// OAuth-signed-in user has switched to via `axilio org use` (AXI-1280).
type Config struct {
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	ActiveOrg string `json:"active_org,omitempty"`
}

// Path is the location of the config file, honoring XDG_CONFIG_HOME.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "axilio", "config.json")
}

// Load reads the config, returning a zero Config when absent or unreadable.
func Load() Config {
	var c Config
	b, err := os.ReadFile(Path())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

// Save writes the config as JSON, readable only by the owner (0600).
func Save(c Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}
