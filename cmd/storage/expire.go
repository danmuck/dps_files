package main

import (
	"fmt"

	"github.com/danmuck/dps_files/src/key_store"
)

func executeExpireAction(cfg RuntimeConfig, ks *key_store.KeyStore) error {
	fmt.Printf("\nSweeping expired files (TTL=%ds)...\n", cfg.TTLSeconds)
	removed := ks.CleanupExpired()
	fmt.Printf("Expired sweep complete: %d file(s) removed.\n", removed)
	return nil
}
