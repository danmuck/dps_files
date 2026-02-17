package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
)

func executeViewAction(cfg RuntimeConfig, ks *key_store.KeyStore) error {
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		fmt.Println("No metadata entries found in storage.")
		return nil
	}

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].FileName == metadata[j].FileName {
			return fmt.Sprintf("%x", metadata[i].FileHash) < fmt.Sprintf("%x", metadata[j].FileHash)
		}
		return metadata[i].FileName < metadata[j].FileName
	})

	fmt.Printf("\nStored metadata entries (%d):\n", len(metadata))
	for i, md := range metadata {
		fmt.Printf("  %d: name=%q hash=%x size=%d chunks=%d modified=%s ttl=%ds\n",
			i,
			md.FileName,
			md.FileHash,
			md.TotalSize,
			md.TotalBlocks,
			formatUnixNano(md.Modified),
			md.TTL,
		)
	}

	selected, selection, err := promptMetadataReassemblySelection(metadata, os.Stdin)
	if err != nil {
		return err
	}
	fmt.Printf("Selection: %s\n", selection)
	if len(selected) == 0 {
		return nil
	}

	for _, md := range selected {
		outputPath := copyOutputPath(cfg.KeyStore.StorageDir, md.FileName)
		if err := createDirPath(filepath.Dir(outputPath)); err != nil {
			return fmt.Errorf("failed to ensure output directory: %w", err)
		}

		fmt.Printf("\nReassembling %q to %s\n", md.FileName, outputPath)
		if err := ks.ReassembleFileToPath(md.FileHash, outputPath); err != nil {
			return fmt.Errorf("failed to reassemble %q: %w", md.FileName, err)
		}
		fmt.Printf("Reassembled: %s\n", outputPath)
	}

	return nil
}

func formatUnixNano(value int64) string {
	if value <= 0 {
		return "unknown"
	}
	return time.Unix(0, value).Format(time.RFC3339)
}
