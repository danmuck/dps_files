package main

import (
	"fmt"
	"os"
	"path/filepath"

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
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		displaySize := md.TotalSize
		if md.IsDirectory() && md.ContentSize > 0 {
			displaySize = md.ContentSize
		}

		var label string
		if md.IsDirectory() {
			label = md.FileName + "  hash: " + shortHash + "...  size: " + formatBytes(displaySize)
		} else {
			label = md.FileName + "  hash: " + shortHash + "...  chunks: " + fmt.Sprintf("%d", md.TotalBlocks) + "  size: " + formatBytes(displaySize)
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
	Entry  RemoteFileEntry
	key    string
	parent string
	label  string
}

func (n remoteTreeNode) TreeLabel() string  { return n.label }
func (n remoteTreeNode) TreeKey() string    { return n.key }
func (n remoteTreeNode) TreeParent() string { return n.parent }

// buildRemoteTreeNodes converts remote entries into tui.TreeNode slice for TreeView.
func buildRemoteTreeNodes(entries []RemoteFileEntry) []tui.TreeNode {
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

	nodes := make([]tui.TreeNode, 0, len(entries))
	for _, e := range entries {
		shortHash := e.Hash
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		var label string
		label = e.Name + "  hash: " + shortHash + "...  size: " + formatBytes(e.Size)

		key := makeNodeKey(e.Size, e.Hash)

		// Match children to parents via ParentHash.
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
	return nodes
}

// makeNodeKey builds a sort key that orders by size descending, then by hash.
func makeNodeKey(size uint64, hash string) string {
	return fmt.Sprintf("%020d:%s", ^size, hash)
}

// localFSTreeNode implements tui.TreeNode for local filesystem entries.
type localFSTreeNode struct {
	Path  string
	Name  string
	IsDir bool
	Size  int64
	key    string
	parent string
	label  string
}

func (n localFSTreeNode) TreeLabel() string  { return n.label }
func (n localFSTreeNode) TreeKey() string    { return n.key }
func (n localFSTreeNode) TreeParent() string { return n.parent }

// buildLocalFSTreeNodes walks rootPath up to 5 levels deep and returns a
// tui.TreeNode slice for rendering with TreeViewTC.
func buildLocalFSTreeNodes(rootPath string) ([]tui.TreeNode, error) {
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
	if err := walkLocalFS(rootPath, rootKey, 0, 5, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// walkLocalFS recursively appends filesystem entries under dirPath to nodes.
func walkLocalFS(dirPath, parentKey string, depth, maxDepth int, nodes *[]tui.TreeNode) error {
	if depth >= maxDepth {
		return nil
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		childPath := filepath.Join(dirPath, e.Name())
		if e.IsDir() {
			childKey := makeNodeKey(^uint64(0), childPath)
			*nodes = append(*nodes, localFSTreeNode{
				Path:   childPath,
				Name:   e.Name(),
				IsDir:  true,
				key:    childKey,
				parent: parentKey,
				label:  e.Name() + "/",
			})
			if err := walkLocalFS(childPath, childKey, depth+1, maxDepth, nodes); err != nil {
				return err
			}
		} else {
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}
			size := uint64(info.Size())
			childKey := makeNodeKey(size, childPath)
			*nodes = append(*nodes, localFSTreeNode{
				Path:   childPath,
				Name:   e.Name(),
				IsDir:  false,
				Size:   info.Size(),
				key:    childKey,
				parent: parentKey,
				label:  e.Name() + "  size: " + formatBytes(size),
			})
		}
	}
	return nil
}
