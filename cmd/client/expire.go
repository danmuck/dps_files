package main

import (
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeExpireAction(cfg RuntimeConfig, ks *key_store.KeyStore) error {
	logs.Printf("\nSweeping expired files (TTL=%ds)...\n", cfg.TTLSeconds)
	removed := ks.CleanupExpired()
	logs.Printf("Expired sweep complete: %d file(s) removed.\n", removed)
	return nil
}
