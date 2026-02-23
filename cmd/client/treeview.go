package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
)

// localTreeNode implements tui.TreeNode for local metadata entries.
type localTreeNode struct {
	MD     key_store.MetaData
	key    string // unique key = inverted-size:hash (used for both identity and sort)
	parent string // parent's key, or "" for root
	label  string
}

func (n localTreeNode) TreeLabel() string  { return n.label }
func (n localTreeNode) TreeKey() string    { return n.key }
func (n localTreeNode) TreeParent() string { return n.parent }

// buildLocalTreeNodes converts metadata into tui.TreeNode slice for TreeView.
func buildLocalTreeNodes(metadata []key_store.MetaData) []tui.TreeNode {
	var zero [key_store.HashSize]byte

	// First pass: build key map for directories so children can reference them.
	dirKeys := make(map[[key_store.HashSize]byte]string) // FileHash → key
	for _, md := range metadata {
		if md.IsDirectory() {
			displaySize := md.ContentSize
			if displaySize == 0 {
				displaySize = md.TotalSize
			}
			dirKeys[md.FileHash] = makeNodeKey(displaySize, fmt.Sprintf("%x", md.FileHash))
		}
	}

	nodes := make([]tui.TreeNode, 0, len(metadata))
	for _, md := range metadata {
		hashHex := fmt.Sprintf("%x", md.FileHash)
		displaySize := md.TotalSize
		if md.IsDirectory() && md.ContentSize > 0 {
			displaySize = md.ContentSize
		}

		var label string
		if md.IsDirectory() {
			label = md.FileName + "  size: " + formatBytes(displaySize)
		} else {
			label = md.FileName + "  chunks: " + fmt.Sprintf("%d", md.TotalBlocks) + "  size: " + formatBytes(displaySize)
		}

		key := makeNodeKey(displaySize, hashHex)

		var parent string
		if !md.IsDirectory() && md.ParentHash != zero {
			if dk, ok := dirKeys[md.ParentHash]; ok {
				parent = dk
			}
		}
		nodes = append(nodes, localTreeNode{
			MD:     md,
			key:    key,
			parent: parent,
			label:  label,
		})
	}
	return nodes
}

// remoteTreeNode implements tui.TreeNode for remote file entries.
type remoteTreeNode struct {
	Entry      RemoteFileEntry
	IsEllipsis bool // true for truncation placeholder nodes
	key        string
	parent     string
	label      string
}

func (n remoteTreeNode) TreeLabel() string  { return n.label }
func (n remoteTreeNode) TreeKey() string    { return n.key }
func (n remoteTreeNode) TreeParent() string { return n.parent }

// buildRemoteTreeNodes converts remote entries into tui.TreeNode slice for TreeView.
// limit caps items shown per parent group (0 = unlimited); truncated groups get an ellipsis node.
func buildRemoteTreeNodes(entries []RemoteFileEntry, limit int) []tui.TreeNode {
	var zeroHash string
	for range 64 {
		zeroHash += "0"
	}

	// Build directory hash → key map for parent lookup.
	dirKeys := make(map[string]string) // hex hash → node key
	for _, e := range entries {
		if e.IsDirectory() {
			dirKeys[e.Hash] = makeNodeKey(e.Size, e.Hash)
		}
	}

	// Group entries by their effective parent key.
	grouped := make(map[string][]RemoteFileEntry) // parentKey → entries
	for _, e := range entries {
		pk := ""
		if !e.IsDirectory() && e.ParentHash != "" && e.ParentHash != zeroHash {
			if dk, ok := dirKeys[e.ParentHash]; ok {
				pk = dk
			}
		}
		grouped[pk] = append(grouped[pk], e)
	}

	nodes := make([]tui.TreeNode, 0, len(entries))
	for parentKey, group := range grouped {
		sort.Slice(group, func(i, j int) bool { return group[i].Size > group[j].Size })
		shown := len(group)
		truncated := 0
		if limit > 0 && len(group) > limit {
			shown = limit
			truncated = len(group) - limit
		}
		for _, e := range group[:shown] {
			label := e.Name + "  size: " + formatBytes(e.Size)
			key := makeNodeKey(e.Size, e.Hash)
			var parent string
			if !e.IsDirectory() && e.ParentHash != "" && e.ParentHash != zeroHash {
				if dk, ok := dirKeys[e.ParentHash]; ok {
					parent = dk
				}
			}
			nodes = append(nodes, remoteTreeNode{
				Entry:  e,
				key:    key,
				parent: parent,
				label:  label,
			})
		}
		if truncated > 0 {
			nodes = append(nodes, remoteTreeNode{
				IsEllipsis: true,
				key:        makeNodeKey(0, parentKey+"/__ellipsis__"),
				parent:     parentKey,
				label:      fmt.Sprintf("... (%d more)", truncated),
			})
		}
	}
	return nodes
}

// makeNodeKey builds a sort key that orders by size descending, then by hash.
func makeNodeKey(size uint64, hash string) string {
	return fmt.Sprintf("%020d:%s", ^size, hash)
}

// localFSTreeNode implements tui.TreeNode for local filesystem entries.
type localFSTreeNode struct {
	Path       string
	Name       string
	IsDir      bool
	IsEllipsis bool // true for truncation placeholder nodes
	Size       int64
	key        string
	parent     string
	label      string
}

func (n localFSTreeNode) TreeLabel() string  { return n.label }
func (n localFSTreeNode) TreeKey() string    { return n.key }
func (n localFSTreeNode) TreeParent() string { return n.parent }

// buildLocalFSTreeNodes walks rootPath up to maxDepth levels deep and returns a
// tui.TreeNode slice for rendering with TreeViewTC. limit caps items shown per
// directory (0 = unlimited); truncated directories get an ellipsis node.
func buildLocalFSTreeNodes(rootPath string, maxDepth, limit int) ([]tui.TreeNode, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}
	rootKey := makeNodeKey(^uint64(0), rootPath)
	nodes := []tui.TreeNode{
		localFSTreeNode{
			Path:   rootPath,
			Name:   info.Name(),
			IsDir:  true,
			key:    rootKey,
			parent: "",
			label:  info.Name() + "/",
		},
	}
	if err := walkLocalFS(rootPath, rootKey, 0, maxDepth, limit, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// walkLocalFS recursively appends filesystem entries under dirPath to nodes.
// Directories appear before files; files are sorted by size descending.
// When limit > 0 and the entry count exceeds it, only the first limit items
// are appended and an ellipsis node is added to indicate the truncation.
func walkLocalFS(dirPath, parentKey string, depth, maxDepth, limit int, nodes *[]tui.TreeNode) error {
	if depth >= maxDepth {
		return nil
	}
	rawEntries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	// Separate dirs (alphabetical from ReadDir) and files (sort by size desc).
	type fileEntry struct {
		e    os.DirEntry
		size int64
	}
	var dirs []os.DirEntry
	var files []fileEntry
	for _, e := range rawEntries {
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}
			files = append(files, fileEntry{e: e, size: info.Size()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].size > files[j].size })

	total := len(dirs) + len(files)
	shown := total
	truncated := 0
	if limit > 0 && total > limit {
		shown = limit
		truncated = total - limit
	}

	// Add dir nodes (up to the limit).
	dirsShown := shown
	if dirsShown > len(dirs) {
		dirsShown = len(dirs)
	}
	var subDirs []string
	for i, e := range dirs {
		if i >= dirsShown {
			break
		}
		childPath := filepath.Join(dirPath, e.Name())
		childKey := makeNodeKey(^uint64(0), childPath)
		*nodes = append(*nodes, localFSTreeNode{
			Path:   childPath,
			Name:   e.Name(),
			IsDir:  true,
			key:    childKey,
			parent: parentKey,
			label:  e.Name() + "/",
		})
		subDirs = append(subDirs, childPath)
	}

	// Add file nodes (remaining slots after dirs).
	filesShown := shown - dirsShown
	for i, fe := range files {
		if i >= filesShown {
			break
		}
		childPath := filepath.Join(dirPath, fe.e.Name())
		size := uint64(fe.size)
		childKey := makeNodeKey(size, childPath)
		*nodes = append(*nodes, localFSTreeNode{
			Path:   childPath,
			Name:   fe.e.Name(),
			IsDir:  false,
			Size:   fe.size,
			key:    childKey,
			parent: parentKey,
			label:  fe.e.Name() + "  size: " + formatBytes(size),
		})
	}

	// Ellipsis node: sorts after all files (key uses size=0 → ^0 = max uint64).
	if truncated > 0 {
		*nodes = append(*nodes, localFSTreeNode{
			IsEllipsis: true,
			key:        makeNodeKey(0, dirPath+"/__ellipsis__"),
			parent:     parentKey,
			label:      fmt.Sprintf("... (%d more)", truncated),
		})
	}

	// Recurse into shown subdirectories.
	for _, subdirPath := range subDirs {
		childKey := makeNodeKey(^uint64(0), subdirPath)
		if err := walkLocalFS(subdirPath, childKey, depth+1, maxDepth, limit, nodes); err != nil {
			return err
		}
	}
	return nil
}
