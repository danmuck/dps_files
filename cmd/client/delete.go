package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
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

	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Remote files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	reader := getBufferedReader(input)
	for {
		t.InputLineFU(fmt.Sprintf("Select file to delete [0-%d] (or e to cancel)", len(tvEntries)-1), "", true)
		logs.Printf("\n")
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
		if convErr != nil || idx < 0 || idx >= len(tvEntries) {
			t.StatusWarnFU(fmt.Sprintf("Invalid selection %q.", choice))
			logs.Printf("\n")
			continue
		}

		entry := tvEntries[idx].Node.(remoteTreeNode).Entry

		// Warn if the name contains "/" — heuristic for directory child.
		if !entry.IsDirectory() && strings.Contains(entry.Name, "/") {
			t.StatusWarnFU(fmt.Sprintf(
				"Warning: %q belongs to a directory. Deleting it will break directory reassembly.",
				entry.Name))
			logs.Printf("\n")
			t.InputLineFU("Continue? [y/N]", "", true)
			logs.Printf("\n")
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
		t.StatusInfoFU(fmt.Sprintf("Deleted %q from remote server.", entry.Name))
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

	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored files (%d)", len(metadata))})
	nodes := buildLocalTreeNodes(metadata)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	reader := getBufferedReader(input)
	var zero [key_store.HashSize]byte
	for {
		t.InputLineFU(fmt.Sprintf("Select file to delete [0-%d] (or e to cancel)", len(tvEntries)-1), "", true)
		logs.Printf("\n")
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
			t.StatusWarnFU(fmt.Sprintf("Invalid selection %q. Enter a numeric index or e.", choice))
			logs.Printf("\n")
			continue
		}
		if idx < 0 || idx >= len(tvEntries) {
			t.StatusWarnFU(fmt.Sprintf("Index %d out of range. Valid range is 0-%d.", idx, len(tvEntries)-1))
			logs.Printf("\n")
			continue
		}

		md := tvEntries[idx].Node.(localTreeNode).MD

		// Warn if this file belongs to a directory manifest.
		if md.ParentHash != zero && !md.IsDirectory() {
			parentName := fmt.Sprintf("%x", md.ParentHash)[:16] + "..."
			for _, e := range tvEntries {
				eMD := e.Node.(localTreeNode).MD
				if eMD.FileHash == md.ParentHash {
					parentName = eMD.FileName
					break
				}
			}
			t.StatusWarnFU(fmt.Sprintf(
				"Warning: %q belongs to directory %q. Deleting it will break directory reassembly.",
				md.FileName, parentName))
			logs.Printf("\n")
			t.InputLineFU("Continue? [y/N]", "", true)
			logs.Printf("\n")
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
		t.StatusInfoFU(fmt.Sprintf("Deleted %q (%d chunk(s) removed).", md.FileName, md.TotalBlocks))
		logs.Printf("\n")
		return nil
	}
}
