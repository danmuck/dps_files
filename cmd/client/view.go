package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func executeRemoteViewAction(cfg RuntimeConfig) error {
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()
	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list remote files: %w", err)
	}
	if len(entries) == 0 {
		logs.Println("No files on remote server.")
		return nil
	}
	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Remote files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries)
	t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})
	return nil
}

func executeViewAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	if cfg.Mode == ModeRemote {
		return executeRemoteViewAction(cfg)
	}
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		logs.Println("No metadata entries found in storage.")
		return nil
	}

	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored metadata entries (%d)", len(metadata))})
	nodes := buildLocalTreeNodes(metadata)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	// Build ordered metadata list matching tree indices.
	orderedMDs := make([]key_store.MetaData, len(tvEntries))
	for i, e := range tvEntries {
		orderedMDs[i] = e.Node.(localTreeNode).MD
	}
	selected, selection, err := promptMetadataReassemblySelection(t, orderedMDs, input)
	if err != nil {
		return err
	}
	logs.Printf("Selection: %s\n", selection)
	if len(selected) == 0 {
		return nil
	}

	for _, md := range selected {
		if md.IsDirectory() {
			outputDir := filepath.Join(cfg.KeyStore.StorageDir, md.FileName)
			logs.Printf("\nReassembling directory %q to %s\n", md.FileName, outputDir)
			if err := ks.ReassembleDirectory(md.FileHash, outputDir); err != nil {
				return fmt.Errorf("failed to reassemble directory %q: %w", md.FileName, err)
			}
			logs.Printf("Reassembled: %s\n", outputDir)
		} else {
			outputPath := filepath.Join(cfg.KeyStore.StorageDir, filepath.Base(md.FileName))
			if err := createDirPath(filepath.Dir(outputPath)); err != nil {
				return fmt.Errorf("failed to ensure output directory: %w", err)
			}
			logs.Printf("\nReassembling %q to %s\n", md.FileName, outputPath)
			if err := ks.ReassembleFileToPath(md.FileHash, outputPath); err != nil {
				return fmt.Errorf("failed to reassemble %q: %w", md.FileName, err)
			}
			logs.Printf("Reassembled: %s\n", outputPath)
		}
	}

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
