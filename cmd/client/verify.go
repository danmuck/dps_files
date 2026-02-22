package main

import (
	"fmt"

	logs "github.com/danmuck/smplog"
)

func executeVerifyAction(cfg RuntimeConfig, client *GRPCClient) error {
	logs.Println("\nRunning integrity scan...")
	issues, err := client.Verify()
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if len(issues) == 0 {
		cfg.TUI.StatusInfoFU("All chunks verified: healthy.")
		logs.Printf("\n")
		return nil
	}
	logs.Printf("Found %d integrity error(s):\n", len(issues))
	for _, iss := range issues {
		cfg.TUI.MenuItemFU(int(iss.ChunkIndex), iss.FileName+" — "+iss.Err, false)
		logs.Printf("\n")
	}
	return nil
}
