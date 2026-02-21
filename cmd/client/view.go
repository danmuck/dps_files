package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeRemoteViewAction(cfg RuntimeConfig) error {
	client := NewFileServerClient(cfg.RemoteAddr)
	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list remote files: %w", err)
	}
	if len(entries) == 0 {
		logs.Println("No files on remote server.")
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	logs.Titlef("\nRemote files (%d):\n", len(entries))
	for i, e := range entries {
		shortHash := e.Hash
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		logs.MenuItem(i, logs.PadRight(30, e.Name)+"  hash: "+shortHash+"...  size: "+formatBytes(e.Size), false)
		logs.Printf("\n")
	}
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

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].FileName == metadata[j].FileName {
			return fmt.Sprintf("%x", metadata[i].FileHash) < fmt.Sprintf("%x", metadata[j].FileHash)
		}
		return metadata[i].FileName < metadata[j].FileName
	})

	logs.Titlef("\nStored metadata entries (%d):\n", len(metadata))
	for i, md := range metadata {
		lastChunk := calculateLastChunkSize(md)
		chunkSize := uint64(md.BlockSize)
		hashHex := fmt.Sprintf("%x", md.FileHash)
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}

		logs.MenuItem(i, md.FileName, false)
		logs.Printf("\n")
		logs.Dataf("      hash: %s...  size: %s  chunks: %d\n", shortHash, formatBytes(md.TotalSize), md.TotalBlocks)
		logs.Dataf("      chunk_size: %s  last_chunk: %s  modified: %s  ttl: %s\n",
			formatBytes(chunkSize),
			formatBytes(lastChunk),
			formatUnixNano(md.Modified),
			formatTTLSeconds(md.TTL),
		)
	}

	selected, selection, err := promptMetadataReassemblySelection(metadata, input)
	if err != nil {
		return err
	}
	logs.Printf("Selection: %s\n", selection)
	if len(selected) == 0 {
		return nil
	}

	for _, md := range selected {
		outputPath := copyOutputPath(cfg.KeyStore.StorageDir, md.FileName)
		if err := createDirPath(filepath.Dir(outputPath)); err != nil {
			return fmt.Errorf("failed to ensure output directory: %w", err)
		}

		logs.Printf("\nReassembling %q to %s\n", md.FileName, outputPath)
		if err := ks.ReassembleFileToPath(md.FileHash, outputPath); err != nil {
			return fmt.Errorf("failed to reassemble %q: %w", md.FileName, err)
		}
		logs.Printf("Reassembled: %s\n", outputPath)
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
