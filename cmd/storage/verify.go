package main

import (
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeVerifyAction(cfg RuntimeConfig, ks *key_store.KeyStore) error {
	logs.Println("\nRunning integrity scan...")
	errs := ks.VerifyAll()
	if len(errs) == 0 {
		logs.Println("All chunks verified: healthy.")
		return nil
	}
	logs.Printf("Found %d integrity error(s):\n", len(errs))
	for _, ce := range errs {
		logs.Dataf("  [chunk %d] %s — %v\n", ce.ChunkIndex, ce.FileName, ce.Err)
	}
	return nil // non-fatal: report errors but don't fail the session
}
