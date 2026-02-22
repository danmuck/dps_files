package main

import (
	"fmt"

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
			label = "[DIR] " + md.FileName + "  hash: " + shortHash + "...  size: " + formatBytes(displaySize)
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
		if parent != "" {
			label = "[^] " + label
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
		if e.IsDirectory() {
			label = "[DIR] " + e.Name + "  hash: " + shortHash + "...  size: " + formatBytes(e.Size)
		} else {
			label = e.Name + "  hash: " + shortHash + "...  size: " + formatBytes(e.Size)
		}

		key := makeNodeKey(e.Size, e.Hash)

		// Match children to parents via ParentHash.
		var parent string
		if !e.IsDirectory() && e.ParentHash != "" && e.ParentHash != zeroHash {
			if dk, ok := dirKeys[e.ParentHash]; ok {
				parent = dk
			}
		}
		if parent != "" {
			label = "[^] " + label
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
