package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

func executeDeleteAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		fmt.Println("No stored files to delete.")
		return nil
	}

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].FileName == metadata[j].FileName {
			return fmt.Sprintf("%x", metadata[i].FileHash) < fmt.Sprintf("%x", metadata[j].FileHash)
		}
		return metadata[i].FileName < metadata[j].FileName
	})

	fmt.Printf("\nStored files (%d):\n", len(metadata))
	for i, md := range metadata {
		hashHex := fmt.Sprintf("%x", md.FileHash)
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		fmt.Printf("  [%d] %s  hash: %s...  chunks: %d  size: %s\n",
			i, md.FileName, shortHash, md.TotalBlocks, formatBytes(md.TotalSize))
	}

	reader := getBufferedReader(input)
	for {
		fmt.Printf("\nSelect file to delete [0-%d] (or e to cancel): ", len(metadata)-1)
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read selection: %w", err)
		}

		choice := strings.TrimSpace(line)
		if strings.EqualFold(choice, "e") {
			return errMenuBack
		}

		idx, convErr := strconv.Atoi(choice)
		if convErr != nil {
			fmt.Printf("Invalid selection %q. Enter a numeric index or e.\n", choice)
			continue
		}
		if idx < 0 || idx >= len(metadata) {
			fmt.Printf("Index %d out of range. Valid range is 0-%d.\n", idx, len(metadata)-1)
			continue
		}

		md := metadata[idx]
		if err := ks.DeleteFile(md.FileHash); err != nil {
			return fmt.Errorf("failed to delete %q: %w", md.FileName, err)
		}
		fmt.Printf("Deleted %q (%d chunk(s) removed).\n", md.FileName, md.TotalBlocks)
		return nil
	}
}
