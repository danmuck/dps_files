package main

import (
	"fmt"

	logs "github.com/danmuck/smplog"
)

func executeExpireAction(cfg RuntimeConfig, client *GRPCClient) error {
	logs.Printf("\nSweeping expired files...\n")
	removed, err := client.Expire()
	if err != nil {
		return fmt.Errorf("expire: %w", err)
	}
	logs.Printf("Expired sweep complete: %d file(s) removed.\n", removed)
	return nil
}
