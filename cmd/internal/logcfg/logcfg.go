package logcfg

import (
	"os"

	logs "github.com/danmuck/smplog"
)

const envConfigPath = "SMPLOG_CONFIG"

// Load returns file-backed logging configuration when available, otherwise defaults.
func Load() logs.Config {
	if path := os.Getenv(envConfigPath); path != "" {
		if cfg, err := logs.ConfigFromFile(path); err == nil {
			return cfg
		}
	}

	candidates := []string{
		"./smplog.config.toml",
		"./local/smplog.config.toml",
	}

	for _, path := range candidates {
		if cfg, err := logs.ConfigFromFile(path); err == nil {
			return cfg
		}
	}

	return logs.DefaultConfig()
}
