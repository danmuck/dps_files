package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeRemoteDeleteAction(cfg RuntimeConfig, input io.Reader) error {
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
		logs.MenuItem(i, e.Name+"  hash: "+shortHash+"...  size: "+formatBytes(e.Size), false)
		logs.Printf("\n")
	}

	reader := getBufferedReader(input)
	for {
		logs.Promptf("\nSelect file to delete [0-%d] (or e to cancel): ", len(entries)-1)
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read selection: %w", err)
		}
		choice := strings.TrimSpace(line)
		if strings.EqualFold(choice, "e") {
			return errMenuBack
		}
		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 0 || idx >= len(entries) {
			logs.StatusWarn(fmt.Sprintf("Invalid selection %q.", choice)); logs.Printf("\n")
			continue
		}
		hash, err := hexToHash(entries[idx].Hash)
		if err != nil {
			return fmt.Errorf("invalid server hash for %q: %w", entries[idx].Name, err)
		}
		if err := client.Delete(hash); err != nil {
			return fmt.Errorf("delete %q: %w", entries[idx].Name, err)
		}
		logs.StatusInfo(fmt.Sprintf("Deleted %q from remote server.", entries[idx].Name)); logs.Printf("\n")
		return nil
	}
}

func executeDeleteAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	if cfg.Mode == ModeRemote {
		return executeRemoteDeleteAction(cfg, input)
	}
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		logs.Println("No stored files to delete.")
		return nil
	}

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].FileName == metadata[j].FileName {
			return fmt.Sprintf("%x", metadata[i].FileHash) < fmt.Sprintf("%x", metadata[j].FileHash)
		}
		return metadata[i].FileName < metadata[j].FileName
	})

	logs.Titlef("\nStored files (%d):\n", len(metadata))
	for i, md := range metadata {
		hashHex := fmt.Sprintf("%x", md.FileHash)
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		logs.MenuItem(i, md.FileName+"  hash: "+shortHash+"...  chunks: "+fmt.Sprintf("%d", md.TotalBlocks)+"  size: "+formatBytes(md.TotalSize), false)
		logs.Printf("\n")
	}

	reader := getBufferedReader(input)
	for {
		logs.Promptf("\nSelect file to delete [0-%d] (or e to cancel): ", len(metadata)-1)
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
			logs.StatusWarn(fmt.Sprintf("Invalid selection %q. Enter a numeric index or e.", choice)); logs.Printf("\n")
			continue
		}
		if idx < 0 || idx >= len(metadata) {
			logs.StatusWarn(fmt.Sprintf("Index %d out of range. Valid range is 0-%d.", idx, len(metadata)-1)); logs.Printf("\n")
			continue
		}

		md := metadata[idx]
		if err := ks.DeleteFile(md.FileHash); err != nil {
			return fmt.Errorf("failed to delete %q: %w", md.FileName, err)
		}
		logs.StatusInfo(fmt.Sprintf("Deleted %q (%d chunk(s) removed).", md.FileName, md.TotalBlocks)); logs.Printf("\n")
		return nil
	}
}
