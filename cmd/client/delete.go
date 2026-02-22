package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func executeDeleteAction(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
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
		t.StatusInfoFU(fmt.Sprintf("Deleted %q from server.", entry.Name))
		logs.Printf("\n")
		return nil
	}
}
