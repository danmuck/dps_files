package main

import (
	"fmt"

	"github.com/danmuck/dps_files/src/key_store"
)

func executeVerifyAction(cfg RuntimeConfig, ks *key_store.KeyStore) error {
	fmt.Println("\nRunning integrity scan...")
	errs := ks.VerifyAll()
	if len(errs) == 0 {
		fmt.Println("All chunks verified: healthy.")
		return nil
	}
	fmt.Printf("Found %d integrity error(s):\n", len(errs))
	for _, ce := range errs {
		fmt.Printf("  [chunk %d] %s — %v\n", ce.ChunkIndex, ce.FileName, ce.Err)
	}
	return nil // non-fatal: report errors but don't fail the session
}
