package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
)

// makeHash returns a [HashSize]byte with b in position 0 — a unique test hash.
func makeHash(b byte) [key_store.HashSize]byte {
	var h [key_store.HashSize]byte
	h[0] = b
	return h
}

// renderTree is a test helper that renders TreeView and returns the flattened entries.
func renderTree(nodes []tui.TreeNode) []tui.TreeViewEntry {
	t := tui.NewTUI(os.Stdout)
	return t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})
}

// --- buildLocalTreeNodes tests ---

func TestLocalTreeNodes_DirectoryBeforeOrphan(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{
		FileName:    "mydir",
		FileHash:    dirHash,
		EntryType:   "directory",
		ContentSize: 2048,
	}
	orphan := key_store.MetaData{
		FileName:  "orphan.txt",
		FileHash:  makeHash(2),
		TotalSize: 512,
	}

	nodes := buildLocalTreeNodes([]key_store.MetaData{orphan, dir})
	entries := renderTree(nodes)

	if len(entries) != 2 {
		t.Fatalf("expected 2 items, got %d", len(entries))
	}
	md0 := entries[0].Node.(localTreeNode).MD
	if !md0.IsDirectory() {
		t.Errorf("expected directory first, got %q", md0.FileName)
	}
	md1 := entries[1].Node.(localTreeNode).MD
	if md1.FileName != "orphan.txt" {
		t.Errorf("expected orphan second, got %q", md1.FileName)
	}
}

func TestLocalTreeNodes_ChildGroupedUnderDirectory(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{
		FileName:    "mydir",
		FileHash:    dirHash,
		EntryType:   "directory",
		ContentSize: 2048,
	}
	child := key_store.MetaData{
		FileName:   "mydir/child.txt",
		FileHash:   makeHash(2),
		ParentHash: dirHash,
		TotalSize:  1024,
	}
	orphan := key_store.MetaData{
		FileName:  "orphan.txt",
		FileHash:  makeHash(3),
		TotalSize: 512,
	}

	nodes := buildLocalTreeNodes([]key_store.MetaData{orphan, child, dir})
	entries := renderTree(nodes)

	if len(entries) != 3 {
		t.Fatalf("expected 3 items, got %d", len(entries))
	}
	md0 := entries[0].Node.(localTreeNode).MD
	if !md0.IsDirectory() {
		t.Errorf("item[0] should be dir, got %q", md0.FileName)
	}
	md1 := entries[1].Node.(localTreeNode).MD
	if md1.FileName != "mydir/child.txt" {
		t.Errorf("item[1] should be child, got %q", md1.FileName)
	}
	if entries[1].Depth == 0 {
		t.Errorf("child should have depth > 0")
	}
	md2 := entries[2].Node.(localTreeNode).MD
	if md2.FileName != "orphan.txt" {
		t.Errorf("item[2] should be orphan, got %q", md2.FileName)
	}
}

func TestLocalTreeNodes_ChildHasTreePrefix(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{FileHash: dirHash, EntryType: "directory", FileName: "d", ContentSize: 500}
	c1 := key_store.MetaData{ParentHash: dirHash, TotalSize: 200, FileName: "d/a", FileHash: makeHash(2)}
	c2 := key_store.MetaData{ParentHash: dirHash, TotalSize: 100, FileName: "d/b", FileHash: makeHash(3)}

	nodes := buildLocalTreeNodes([]key_store.MetaData{dir, c2, c1})
	entries := renderTree(nodes)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Children should have non-empty prefix (tree connectors)
	if entries[1].Prefix == "" {
		t.Errorf("non-last child should have tree prefix")
	}
	if entries[2].Prefix == "" {
		t.Errorf("last child should have tree prefix")
	}
}

func TestLocalTreeNodes_SizeDescendingOrphans(t *testing.T) {
	small := key_store.MetaData{FileName: "small.txt", FileHash: makeHash(1), TotalSize: 100}
	big := key_store.MetaData{FileName: "big.txt", FileHash: makeHash(2), TotalSize: 1000}

	nodes := buildLocalTreeNodes([]key_store.MetaData{small, big})
	entries := renderTree(nodes)

	md0 := entries[0].Node.(localTreeNode).MD
	if md0.FileName != "big.txt" {
		t.Errorf("expected big.txt first (size desc), got %q", md0.FileName)
	}
}

func TestLocalTreeNodes_SizeDescendingDirs(t *testing.T) {
	small := key_store.MetaData{FileName: "small", FileHash: makeHash(1), EntryType: "directory", ContentSize: 100}
	big := key_store.MetaData{FileName: "big", FileHash: makeHash(2), EntryType: "directory", ContentSize: 1000}

	nodes := buildLocalTreeNodes([]key_store.MetaData{small, big})
	entries := renderTree(nodes)

	md0 := entries[0].Node.(localTreeNode).MD
	if md0.FileName != "big" {
		t.Errorf("expected big dir first, got %q", md0.FileName)
	}
}

func TestLocalTreeNodes_SequentialIndices(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{FileHash: dirHash, EntryType: "directory", FileName: "d", ContentSize: 500}
	child := key_store.MetaData{ParentHash: dirHash, TotalSize: 200, FileName: "d/c", FileHash: makeHash(2)}
	orphan := key_store.MetaData{FileName: "f.txt", FileHash: makeHash(3), TotalSize: 50}

	nodes := buildLocalTreeNodes([]key_store.MetaData{orphan, child, dir})
	entries := renderTree(nodes)

	for i, e := range entries {
		if e.Index != i {
			t.Errorf("entry[%d].Index = %d, want %d", i, e.Index, i)
		}
	}
}

// --- buildRemoteTreeNodes tests ---

func TestRemoteTreeNodes_DirectoryBeforeOrphan(t *testing.T) {
	dir := RemoteFileEntry{Name: "mydir", EntryType: "directory", Size: 2048, Hash: fmt.Sprintf("%x", makeHash(1))}
	orphan := RemoteFileEntry{Name: "orphan.txt", Size: 512, Hash: fmt.Sprintf("%x", makeHash(2))}

	nodes := buildRemoteTreeNodes([]RemoteFileEntry{orphan, dir})
	entries := renderTree(nodes)

	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
	e0 := entries[0].Node.(remoteTreeNode).Entry
	if !e0.IsDirectory() {
		t.Errorf("expected directory first")
	}
	e1 := entries[1].Node.(remoteTreeNode).Entry
	if e1.Name != "orphan.txt" {
		t.Errorf("expected orphan second, got %q", e1.Name)
	}
}

func TestRemoteTreeNodes_ChildGroupedUnderDirectory(t *testing.T) {
	dirHash := fmt.Sprintf("%x", makeHash(1))
	dir := RemoteFileEntry{Name: "mydir", EntryType: "directory", Size: 2048, Hash: dirHash}
	child := RemoteFileEntry{Name: "child.txt", Size: 1024, Hash: fmt.Sprintf("%x", makeHash(2)), ParentHash: dirHash}
	orphan := RemoteFileEntry{Name: "orphan.txt", Size: 512, Hash: fmt.Sprintf("%x", makeHash(3))}

	nodes := buildRemoteTreeNodes([]RemoteFileEntry{orphan, child, dir})
	entries := renderTree(nodes)

	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	e1 := entries[1].Node.(remoteTreeNode).Entry
	if e1.Name != "child.txt" {
		t.Errorf("expected child at [1], got %q", e1.Name)
	}
	if entries[1].Depth == 0 {
		t.Errorf("child should have depth > 0")
	}
	e2 := entries[2].Node.(remoteTreeNode).Entry
	if e2.Name != "orphan.txt" {
		t.Errorf("expected orphan at [2], got %q", e2.Name)
	}
}

func TestRemoteTreeNodes_ChildHasTreePrefix(t *testing.T) {
	dirHash := fmt.Sprintf("%x", makeHash(1))
	dir := RemoteFileEntry{Name: "d", EntryType: "directory", Size: 500, Hash: dirHash}
	c1 := RemoteFileEntry{Name: "a", Size: 200, Hash: fmt.Sprintf("%x", makeHash(2)), ParentHash: dirHash}
	c2 := RemoteFileEntry{Name: "b", Size: 100, Hash: fmt.Sprintf("%x", makeHash(3)), ParentHash: dirHash}

	nodes := buildRemoteTreeNodes([]RemoteFileEntry{dir, c2, c1})
	entries := renderTree(nodes)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[1].Prefix == "" {
		t.Errorf("non-last child should have tree prefix")
	}
	if entries[2].Prefix == "" {
		t.Errorf("last child should have tree prefix")
	}
}

func TestRemoteTreeNodes_SequentialIndices(t *testing.T) {
	dirHash := fmt.Sprintf("%x", makeHash(1))
	dir := RemoteFileEntry{Name: "d", EntryType: "directory", Size: 500, Hash: dirHash}
	child := RemoteFileEntry{Name: "c", Size: 200, Hash: fmt.Sprintf("%x", makeHash(2)), ParentHash: dirHash}
	orphan := RemoteFileEntry{Name: "f.txt", Size: 50, Hash: fmt.Sprintf("%x", makeHash(3))}

	nodes := buildRemoteTreeNodes([]RemoteFileEntry{orphan, child, dir})
	entries := renderTree(nodes)

	for i, e := range entries {
		if e.Index != i {
			t.Errorf("entry[%d].Index = %d, want %d", i, e.Index, i)
		}
	}
}
