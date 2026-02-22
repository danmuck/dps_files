package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func executeViewAction(cfg RuntimeConfig, client *GRPCClient) error {
	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}
	if len(entries) == 0 {
		logs.Println("No files on server.")
		return nil
	}
	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries)
	t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})
	return nil
}

func formatUnixNano(value int64) string {
	if value <= 0 {
		return "unknown"
	}
	return time.Unix(0, value).Format(time.RFC3339)
}

func formatTTLSeconds(seconds uint64) string {
	if seconds == 0 {
		return "0s"
	}
	return time.Duration(seconds * uint64(time.Second)).String()
}

func calculateLastChunkSize(md key_store.MetaData) uint64 {
	if md.TotalBlocks == 0 || md.BlockSize == 0 {
		return 0
	}
	if md.TotalBlocks == 1 {
		return md.TotalSize
	}
	fullBlocks := uint64(md.BlockSize) * uint64(md.TotalBlocks-1)
	if md.TotalSize <= fullBlocks {
		return md.TotalSize
	}
	return md.TotalSize - fullBlocks
}

func formatBytes(value uint64) string {
	if value == 0 {
		return "0 B"
	}

	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	unitIdx := 0
	for size >= 1024 && unitIdx < len(units)-1 {
		size /= 1024
		unitIdx++
	}

	if unitIdx == 0 {
		return fmt.Sprintf("%d %s", value, units[unitIdx])
	}

	formatted := fmt.Sprintf("%.2f", size)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	return fmt.Sprintf("%s %s", formatted, units[unitIdx])
}
