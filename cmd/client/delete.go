package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeRemoteDeleteAction(cfg RuntimeConfig, input io.Reader) error {
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

	items := buildRemoteTree(entries)
	logs.Titlef("\nRemote files (%d):\n", len(items))
	for _, it := range items {
		shortHash := it.Entry.Hash
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		displayName := it.Prefix
		if it.Entry.IsDirectory() {
			displayName += "[DIR] " + it.Entry.Name
		} else {
			displayName += it.Entry.Name
		}
		logs.MenuItem(it.Idx, displayName+"  hash: "+shortHash+"...  size: "+formatBytes(it.Entry.Size), false)
		logs.Printf("\n")
	}

	reader := getBufferedReader(input)
	for {
		logs.Promptf("\nSelect file to delete [0-%d] (or e to cancel): ", len(items)-1)
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
		if convErr != nil || idx < 0 || idx >= len(items) {
			logs.StatusWarn(fmt.Sprintf("Invalid selection %q.", choice))
			logs.Printf("\n")
			continue
		}

		entry := items[idx].Entry

		// Warn if the name contains "/" — heuristic for directory child.
		if !entry.IsDirectory() && strings.Contains(entry.Name, "/") {
			logs.StatusWarn(fmt.Sprintf(
				"Warning: %q belongs to a directory. Deleting it will break directory reassembly.",
				entry.Name))
			logs.Printf("\n")
			logs.Promptf("Continue? [y/N]: ")
			confirmLine, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return fmt.Errorf("read confirmation: %w", confirmErr)
			}
			if c := strings.ToLower(strings.TrimSpace(confirmLine)); c != "y" && c != "yes" {
				logs.Println("Delete cancelled.")
				continue
			}
		}

		hash, err := hexToHash(entry.Hash)
		if err != nil {
			return fmt.Errorf("invalid server hash for %q: %w", entry.Name, err)
		}
		if err := client.Delete(hash); err != nil {
			return fmt.Errorf("delete %q: %w", entry.Name, err)
		}
		logs.StatusInfo(fmt.Sprintf("Deleted %q from remote server.", entry.Name))
		logs.Printf("\n")
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

	items := buildLocalTree(metadata)
	logs.Titlef("\nStored files (%d):\n", len(items))
	for _, it := range items {
		hashHex := fmt.Sprintf("%x", it.MD.FileHash)
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		displayName := it.Prefix
		if it.MD.IsDirectory() {
			displayName += "[DIR] " + it.MD.FileName
		} else {
			displayName += it.MD.FileName
		}
		displaySize := it.MD.TotalSize
		if it.MD.IsDirectory() && it.MD.ContentSize > 0 {
			displaySize = it.MD.ContentSize
		}
		logs.MenuItem(it.Idx, displayName+"  hash: "+shortHash+"...  chunks: "+fmt.Sprintf("%d", it.MD.TotalBlocks)+"  size: "+formatBytes(displaySize), false)
		logs.Printf("\n")
	}

	reader := getBufferedReader(input)
	var zero [key_store.HashSize]byte
	for {
		logs.Promptf("\nSelect file to delete [0-%d] (or e to cancel): ", len(items)-1)
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
			logs.StatusWarn(fmt.Sprintf("Invalid selection %q. Enter a numeric index or e.", choice))
			logs.Printf("\n")
			continue
		}
		if idx < 0 || idx >= len(items) {
			logs.StatusWarn(fmt.Sprintf("Index %d out of range. Valid range is 0-%d.", idx, len(items)-1))
			logs.Printf("\n")
			continue
		}

		md := items[idx].MD

		// Warn if this file belongs to a directory manifest.
		if md.ParentHash != zero && !md.IsDirectory() {
			parentName := fmt.Sprintf("%x", md.ParentHash)[:16] + "..."
			for _, it := range items {
				if it.MD.FileHash == md.ParentHash {
					parentName = it.MD.FileName
					break
				}
			}
			logs.StatusWarn(fmt.Sprintf(
				"Warning: %q belongs to directory %q. Deleting it will break directory reassembly.",
				md.FileName, parentName))
			logs.Printf("\n")
			logs.Promptf("Continue? [y/N]: ")
			confirmLine, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return fmt.Errorf("read confirmation: %w", confirmErr)
			}
			if c := strings.ToLower(strings.TrimSpace(confirmLine)); c != "y" && c != "yes" {
				logs.Println("Delete cancelled.")
				continue
			}
		}

		if err := ks.DeleteFile(md.FileHash); err != nil {
			return fmt.Errorf("failed to delete %q: %w", md.FileName, err)
		}
		logs.StatusInfo(fmt.Sprintf("Deleted %q (%d chunk(s) removed).", md.FileName, md.TotalBlocks))
		logs.Printf("\n")
		return nil
	}
}
