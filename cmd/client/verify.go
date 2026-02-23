package main

import (
	"fmt"
	"io"
	"strings"

	logs "github.com/danmuck/smplog"
)

func executeVerifyAction(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
	logs.Println("\nRunning integrity scan...")
	issues, err := client.Verify()
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if len(issues) == 0 {
		cfg.TUI.StatusInfoFU("All chunks verified: healthy.")
		logs.Printf("\n")
	} else {
		logs.Printf("Found %d integrity error(s):\n", len(issues))
		for _, iss := range issues {
			cfg.TUI.MenuItemFU(int(iss.ChunkIndex), iss.FileName+" — "+iss.Err, false)
			logs.Printf("\n")
		}
	}

	reader := getBufferedReader(input)
	entries, listErr := client.List()
	if listErr == nil {
		orphans := detectOrphans(entries)
		if len(orphans) > 0 {
			logs.Printf("\n")
			cfg.TUI.StatusWarnFU(fmt.Sprintf("Found %d orphaned file(s) with no parent directory:", len(orphans)))
			logs.Printf("\n")
			for i, o := range orphans {
				cfg.TUI.MenuItemFU(i, o.Name+"  "+formatBytes(o.Size), false)
				logs.Printf("\n")
			}
			cfg.TUI.InputLineFU("Remove all orphaned files? [y/N]", "", true)
			logs.Printf("\n")
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) == "y" {
				removed := 0
				for _, o := range orphans {
					if h, err := hexToHash(o.Hash); err == nil {
						if err := client.Delete(h); err == nil {
							removed++
						}
					}
				}
				cfg.TUI.StatusInfoFU(fmt.Sprintf("Removed %d orphaned file(s).", removed))
				logs.Printf("\n")
			}
		}
	}

	return nil
}
