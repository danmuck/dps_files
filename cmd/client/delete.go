package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	reader := getBufferedReader(input)
	t.InputLineFU("Tree options [-l N limit, Enter to skip]", "", true)
	logs.Printf("\n")
	optLine, _ := reader.ReadString('\n')
	_, _, limit := parseFSFlags(strings.TrimSpace(optLine))

	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries, limit)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	for {
		t.InputLineFU(fmt.Sprintf("Select [0-%d], range N-M, or e to cancel", len(tvEntries)-1), "", true)
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

		// Range selection: N-M
		if lo, hi, ok := parseIndexRange(choice, len(tvEntries)-1); ok {
			// Collect non-ellipsis nodes in the range.
			type target struct {
				entry remoteTreeNode
				idx   int
			}
			var targets []target
			for i := lo; i <= hi; i++ {
				n := tvEntries[i].Node.(remoteTreeNode)
				if !n.IsEllipsis {
					targets = append(targets, target{entry: n, idx: i})
				}
			}
			if len(targets) == 0 {
				t.StatusWarnFU("Range contains only placeholder entries.")
				logs.Printf("\n")
				continue
			}
			t.InputLineFU(fmt.Sprintf("Delete %d item(s) in range [%d-%d]? [y/N]", len(targets), lo, hi), "", true)
			logs.Printf("\n")
			confirmLine, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return fmt.Errorf("read confirmation: %w", confirmErr)
			}
			if c := strings.ToLower(strings.TrimSpace(confirmLine)); c != "y" && c != "yes" {
				logs.Println("Delete cancelled.")
				continue
			}
			removed := 0
			for _, tgt := range targets {
				entry := tgt.entry.Entry
				hash, hashErr := hexToHash(entry.Hash)
				if hashErr != nil {
					logs.Printf("Skipping %q: invalid hash: %v\n", entry.Name, hashErr)
					continue
				}
				if entry.IsDirectory() {
					if delErr := deleteDirectoryRecursive(client, hash); delErr != nil {
						logs.Printf("Failed to delete directory %q: %v\n", entry.Name, delErr)
						continue
					}
				} else {
					if delErr := client.Delete(hash); delErr != nil {
						logs.Printf("Failed to delete %q: %v\n", entry.Name, delErr)
						continue
					}
				}
				removed++
			}
			t.StatusInfoFU(fmt.Sprintf("Deleted %d/%d item(s).", removed, len(targets)))
			logs.Printf("\n")
			return nil
		}

		// Single index
		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 0 || idx >= len(tvEntries) {
			t.StatusWarnFU(fmt.Sprintf("Invalid selection %q.", choice))
			logs.Printf("\n")
			continue
		}

		node := tvEntries[idx].Node.(remoteTreeNode)
		if node.IsEllipsis {
			t.StatusWarnFU("That entry is a placeholder — select a file or directory.")
			logs.Printf("\n")
			continue
		}
		entry := node.Entry

		hash, err := hexToHash(entry.Hash)
		if err != nil {
			return fmt.Errorf("invalid server hash for %q: %w", entry.Name, err)
		}

		if entry.IsDirectory() {
			t.InputLineFU(fmt.Sprintf("Delete directory %q and all its contents? [y/N]", entry.Name), "", true)
			logs.Printf("\n")
			confirmLine, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return fmt.Errorf("read confirmation: %w", confirmErr)
			}
			if c := strings.ToLower(strings.TrimSpace(confirmLine)); c != "y" && c != "yes" {
				logs.Println("Delete cancelled.")
				continue
			}
			if err := deleteDirectoryRecursive(client, hash); err != nil {
				return fmt.Errorf("recursive delete %q: %w", entry.Name, err)
			}
			t.StatusInfoFU(fmt.Sprintf("Deleted directory %q and all its contents.", entry.Name))
			logs.Printf("\n")
			return nil
		}

		// Warn if the name contains "/" — heuristic for directory child.
		if strings.Contains(entry.Name, "/") {
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

		if err := client.Delete(hash); err != nil {
			return fmt.Errorf("delete %q: %w", entry.Name, err)
		}
		t.StatusInfoFU(fmt.Sprintf("Deleted %q from server.", entry.Name))
		logs.Printf("\n")
		return nil
	}
}

// parseIndexRange parses "lo-hi" (both non-negative integers, lo <= hi <= max).
// Returns (lo, hi, true) on success, (0, 0, false) otherwise.
func parseIndexRange(s string, max int) (lo, hi int, ok bool) {
	dash := strings.Index(s, "-")
	if dash <= 0 {
		return 0, 0, false
	}
	l, err1 := strconv.Atoi(s[:dash])
	h, err2 := strconv.Atoi(s[dash+1:])
	if err1 != nil || err2 != nil || l < 0 || h > max || l > h {
		return 0, 0, false
	}
	return l, h, true
}

// deleteDirectoryRecursive deletes all children of a directory manifest, then the manifest itself.
// Children that are already absent (NotFound) are skipped — the manifest may reference files
// that were deleted independently, and those gaps should not block the rest of the cleanup.
func deleteDirectoryRecursive(client *GRPCClient, hash [32]byte) error {
	children, err := client.ListDir(hash)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil // manifest already gone
		}
		return fmt.Errorf("list dir: %w", err)
	}
	for _, child := range children {
		if child.Type == "directory" {
			if err := deleteDirectoryRecursive(client, child.Hash); err != nil {
				return err
			}
		} else {
			if err := client.Delete(child.Hash); err != nil {
				if status.Code(err) == codes.NotFound {
					continue // child already gone — keep deleting the rest
				}
				return fmt.Errorf("delete child %q: %w", child.Name, err)
			}
		}
	}
	return client.Delete(hash)
}

// detectOrphans returns entries whose ParentHash references a non-existent directory manifest.
func detectOrphans(entries []RemoteFileEntry) []RemoteFileEntry {
	zeroHash := strings.Repeat("0", 64)
	dirHashes := make(map[string]bool)
	for _, e := range entries {
		if e.IsDirectory() {
			dirHashes[e.Hash] = true
		}
	}
	var orphans []RemoteFileEntry
	for _, e := range entries {
		if !e.IsDirectory() && e.ParentHash != "" && e.ParentHash != zeroHash {
			if !dirHashes[e.ParentHash] {
				orphans = append(orphans, e)
			}
		}
	}
	return orphans
}
