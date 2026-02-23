package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	logs "github.com/danmuck/smplog"
)

func executeVerifyAction(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
	logs.Println("\nRunning integrity scan...")
	issues, err := client.Verify()
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if len(issues) == 0 {
		cfg.TUI.StatusInfoFU("All chunks verified: healthy.")
		logs.Printf("\n")
	} else {
		logs.Printf("Found %d integrity error(s):\n", len(issues))
		for _, iss := range issues {
			cfg.TUI.MenuItemFU(int(iss.ChunkIndex), iss.FileName+" — "+iss.Err, false)
			logs.Printf("\n")
		}
	}

	reader := getBufferedReader(input)
	entries, listErr := client.List()
	if listErr == nil {
		// --- orphan detection (files with no parent dir) ---
		orphans := detectOrphans(entries)
		if len(orphans) > 0 {
			logs.Printf("\n")
			cfg.TUI.StatusWarnFU(fmt.Sprintf("Found %d orphaned file(s) with no parent directory:", len(orphans)))
			logs.Printf("\n")
			for i, o := range orphans {
				cfg.TUI.MenuItemFU(i, o.Name+"  "+formatBytes(o.Size), false)
				logs.Printf("\n")
			}
			cfg.TUI.InputLineFU("Remove all orphaned files? [y/N]", "", true)
			logs.Printf("\n")
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) == "y" {
				removed := 0
				for _, o := range orphans {
					if h, err := hexToHash(o.Hash); err == nil {
						if err := client.Delete(h); err == nil {
							removed++
						}
					}
				}
				cfg.TUI.StatusInfoFU(fmt.Sprintf("Removed %d orphaned file(s).", removed))
				logs.Printf("\n")
			}
		}

		// --- broken directory detection (dirs missing one or more listed children) ---
		broken := detectBrokenDirectories(entries, client)
		if len(broken) > 0 {
			logs.Printf("\n")
			cfg.TUI.StatusWarnFU(fmt.Sprintf("Found %d broken director(y/ies) with missing children:", len(broken)))
			logs.Printf("\n")
			for _, bd := range broken {
				cfg.TUI.StatusWarnFU(fmt.Sprintf("Directory %q — %d of %d child(ren) missing:",
					bd.entry.Name, len(bd.missing), len(bd.missing)+len(bd.present)))
				logs.Printf("\n")
				for i, m := range bd.missing {
					cfg.TUI.MenuItemFU(i, "missing: "+m.Path, false)
					logs.Printf("\n")
				}

				if len(bd.present) > 0 {
					cfg.TUI.InputLineFU("[1] delete dir+contents  [2] do nothing (default)  [3] revise manifest", "", true)
				} else {
					cfg.TUI.InputLineFU("[1] delete empty dir  [2] do nothing (default)", "", true)
				}
				logs.Printf("\n")
				line, _ := reader.ReadString('\n')
				choice := strings.TrimSpace(line)

				switch choice {
				case "1":
					removed := 0
					for _, child := range bd.present {
						if child.Type == "directory" {
							if err := deleteDirectoryRecursive(client, child.Hash); err != nil {
								logs.Printf("Failed to delete subdir %q: %v\n", child.Name, err)
							} else {
								removed++
							}
						} else {
							if err := client.Delete(child.Hash); err != nil {
								logs.Printf("Failed to delete child %q: %v\n", child.Name, err)
							} else {
								removed++
							}
						}
					}
					if err := client.Delete(bd.dirHash); err != nil {
						logs.Printf("Failed to delete directory manifest %q: %v\n", bd.entry.Name, err)
					} else {
						cfg.TUI.StatusInfoFU(fmt.Sprintf("Deleted %q and %d present child(ren).", bd.entry.Name, removed))
						logs.Printf("\n")
					}
				case "3":
					if len(bd.present) == 0 {
						cfg.TUI.StatusWarnFU("No present children — use option 1 to delete the empty manifest.")
						logs.Printf("\n")
						continue
					}
					newJSON, buildErr := buildRevisedManifest(bd.entry.Name, bd.present)
					if buildErr != nil {
						logs.Printf("Failed to build revised manifest: %v\n", buildErr)
						continue
					}
					if err := client.Delete(bd.dirHash); err != nil {
						logs.Printf("Failed to delete old manifest: %v\n", err)
						continue
					}
					newHash, uploadErr := client.UploadDirManifest(newJSON)
					if uploadErr != nil {
						logs.Printf("Failed to upload revised manifest: %v\n", uploadErr)
						continue
					}
					cfg.TUI.StatusInfoFU(fmt.Sprintf("Revised %q — removed %d missing entr(y/ies). New hash: %x…",
						bd.entry.Name, len(bd.missing), newHash[:4]))
					logs.Printf("\n")
				default: // "2" or empty → do nothing
					logs.Println("No changes made.")
				}
			}
		}
	}

	return nil
}

// brokenDirectory describes a directory manifest that references one or more
// children that no longer exist on the server.
type brokenDirectory struct {
	entry   RemoteFileEntry
	dirHash [32]byte
	missing []RemoteDirEntry // children in manifest but absent from server
	present []RemoteDirEntry // children in manifest and confirmed on server
}

// detectBrokenDirectories returns directories whose manifests reference
// children not present in the server's file list.
func detectBrokenDirectories(entries []RemoteFileEntry, client *GRPCClient) []brokenDirectory {
	// Build a set of known hashes for O(1) lookup.
	known := make(map[[32]byte]bool, len(entries))
	for _, e := range entries {
		if h, err := hexToHash(e.Hash); err == nil {
			known[h] = true
		}
	}

	var result []brokenDirectory
	for _, e := range entries {
		if !e.IsDirectory() {
			continue
		}
		dirHash, err := hexToHash(e.Hash)
		if err != nil {
			continue
		}
		children, err := client.ListDir(dirHash)
		if err != nil {
			continue
		}
		var missing, present []RemoteDirEntry
		for _, child := range children {
			if known[child.Hash] {
				present = append(present, child)
			} else {
				missing = append(missing, child)
			}
		}
		if len(missing) > 0 {
			result = append(result, brokenDirectory{
				entry:   e,
				dirHash: dirHash,
				missing: missing,
				present: present,
			})
		}
	}
	return result
}

// manifestChild mirrors key_store.DirectoryEntry for JSON round-tripping.
type manifestChild struct {
	Name string   `json:"name"`
	Path string   `json:"path"`
	Hash [32]byte `json:"hash"`
	Type string   `json:"type"`
	Size uint64   `json:"size"`
}

// manifestDoc mirrors key_store.DirectoryManifest for JSON round-tripping.
type manifestDoc struct {
	Path      string           `json:"path"`
	Children  []manifestChild  `json:"children"`
	TotalSize uint64           `json:"total_size"`
}

// buildRevisedManifest constructs a DirectoryManifest JSON blob containing
// only the supplied present children, suitable for UploadDirManifest.
func buildRevisedManifest(dirPath string, present []RemoteDirEntry) ([]byte, error) {
	children := make([]manifestChild, len(present))
	var total uint64
	for i, c := range present {
		children[i] = manifestChild{
			Name: c.Name,
			Path: c.Path,
			Hash: c.Hash,
			Type: c.Type,
			Size: c.Size,
		}
		total += c.Size
	}
	doc := manifestDoc{
		Path:      dirPath,
		Children:  children,
		TotalSize: total,
	}
	return json.Marshal(doc)
}
