// Package config is softgaze's configuration (~/.softgaze/config.json) and
// its defaults, in one place for the server and the service installers.
package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// DefaultPort is where softgaze listens unless the config says otherwise.
// claumon uses 3131: the two must not clash.
const DefaultPort = 3133

// Config lives in ~/.softgaze/config.json; every field is optional.
type Config struct {
	Port      int    `json:"port"`
	ClaudeDir string `json:"claude_dir"`
	DBPath    string `json:"db_path"`
	// The minutes counter (which watches the window in front), the break
	// line and today's minutes. On unless set to false.
	Wellbeing *bool `json:"wellbeing,omitempty"`
}

// WellbeingOn reports whether the wellbeing extras are on (the default).
func (c Config) WellbeingOn() bool { return c.Wellbeing == nil || *c.Wellbeing }

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Dir is softgaze's data directory, ~/.softgaze.
func Dir() string { return filepath.Join(home(), ".softgaze") }

// LogPath is where the service writes its log.
func LogPath() string { return filepath.Join(Dir(), "softgaze.log") }

// Load reads the config, with defaults for what it leaves out. A missing
// file gives the defaults; one that does not parse is logged and ignored.
func Load() Config { return load(Dir(), home()) }

func load(dir, home string) Config {
	cfg := Config{}
	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[config] Failed to parse config file: %v (using defaults)", err)
			cfg = Config{}
		}
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.ClaudeDir == "" {
		cfg.ClaudeDir = filepath.Join(home, ".claude")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(dir, "focus.db")
	}
	return cfg
}
