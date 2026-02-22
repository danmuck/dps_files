package tuicfg

import (
	"os"

	tui "github.com/danmuck/tui_go"
)

const envConfigPath = "TUI_CONFIG"

// Load returns file-backed TUI configuration when available, otherwise defaults.
func Load() tui.Config {
	if path := os.Getenv(envConfigPath); path != "" {
		if cfg, err := tui.ConfigFromFile(path); err == nil {
			return cfg
		}
	}

	candidates := []string{
		"./tui.config.toml",
		"./local/tui.config.toml",
	}

	for _, path := range candidates {
		if cfg, err := tui.ConfigFromFile(path); err == nil {
			return cfg
		}
	}

	return tui.DefaultConfig()
}
