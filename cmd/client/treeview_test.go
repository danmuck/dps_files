package main

import (
	"testing"

	"github.com/danmuck/dps_files/src/key_store"
)

// makeHash returns a [HashSize]byte with b in position 0 — a unique test hash.
func makeHash(b byte) [key_store.HashSize]byte {
	var h [key_store.HashSize]byte
	h[0] = b
	return h
}

// --- buildLocalTree tests ---

func TestBuildLocalTree_DirectoryBeforeOrphan(t *testing.T) {
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

	items := buildLocalTree([]key_store.MetaData{orphan, dir})

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].MD.IsDirectory() {
		t.Errorf("expected directory first, got %q", items[0].MD.FileName)
	}
	if items[1].MD.FileName != "orphan.txt" {
		t.Errorf("expected orphan second, got %q", items[1].MD.FileName)
	}
}

func TestBuildLocalTree_ChildGroupedUnderDirectory(t *testing.T) {
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

	items := buildLocalTree([]key_store.MetaData{orphan, child, dir})

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if !items[0].MD.IsDirectory() {
		t.Errorf("item[0] should be dir, got %q", items[0].MD.FileName)
	}
	if items[1].MD.FileName != "mydir/child.txt" {
		t.Errorf("item[1] should be child, got %q", items[1].MD.FileName)
	}
	if items[1].Prefix == "" {
		t.Errorf("child should have a non-empty indent prefix")
	}
	if items[2].MD.FileName != "orphan.txt" {
		t.Errorf("item[2] should be orphan, got %q", items[2].MD.FileName)
	}
}

func TestBuildLocalTree_LastChildGetsCornerPrefix(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{FileHash: dirHash, EntryType: "directory", FileName: "d"}
	c1 := key_store.MetaData{ParentHash: dirHash, TotalSize: 200, FileName: "d/a", FileHash: makeHash(2)}
	c2 := key_store.MetaData{ParentHash: dirHash, TotalSize: 100, FileName: "d/b", FileHash: makeHash(3)}

	items := buildLocalTree([]key_store.MetaData{dir, c2, c1})
	// items: [dir, c1(200), c2(100)]
	if items[1].Prefix != "  ├─ " {
		t.Errorf("non-last child prefix: got %q, want %q", items[1].Prefix, "  ├─ ")
	}
	if items[2].Prefix != "  └─ " {
		t.Errorf("last child prefix: got %q, want %q", items[2].Prefix, "  └─ ")
	}
}

func TestBuildLocalTree_SizeDescendingOrphans(t *testing.T) {
	small := key_store.MetaData{FileName: "small.txt", FileHash: makeHash(1), TotalSize: 100}
	big := key_store.MetaData{FileName: "big.txt", FileHash: makeHash(2), TotalSize: 1000}

	items := buildLocalTree([]key_store.MetaData{small, big})

	if items[0].MD.FileName != "big.txt" {
		t.Errorf("expected big.txt first (size desc), got %q", items[0].MD.FileName)
	}
}

func TestBuildLocalTree_SizeDescendingDirs(t *testing.T) {
	small := key_store.MetaData{FileName: "small", FileHash: makeHash(1), EntryType: "directory", ContentSize: 100}
	big := key_store.MetaData{FileName: "big", FileHash: makeHash(2), EntryType: "directory", ContentSize: 1000}

	items := buildLocalTree([]key_store.MetaData{small, big})

	if items[0].MD.FileName != "big" {
		t.Errorf("expected big dir first, got %q", items[0].MD.FileName)
	}
}

func TestBuildLocalTree_SequentialIndices(t *testing.T) {
	dirHash := makeHash(1)
	dir := key_store.MetaData{FileHash: dirHash, EntryType: "directory", FileName: "d", ContentSize: 500}
	child := key_store.MetaData{ParentHash: dirHash, TotalSize: 200, FileName: "d/c", FileHash: makeHash(2)}
	orphan := key_store.MetaData{FileName: "f.txt", FileHash: makeHash(3), TotalSize: 50}

	items := buildLocalTree([]key_store.MetaData{orphan, child, dir})

	for i, it := range items {
		if it.Idx != i {
			t.Errorf("item[%d].Idx = %d, want %d", i, it.Idx, i)
		}
	}
}

// --- buildRemoteTree tests ---

func TestBuildRemoteTree_DirectoryBeforeOrphan(t *testing.T) {
	dir := RemoteFileEntry{Name: "mydir", EntryType: "directory", Size: 2048}
	orphan := RemoteFileEntry{Name: "orphan.txt", Size: 512}

	items := buildRemoteTree([]RemoteFileEntry{orphan, dir})

	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if !items[0].Entry.IsDirectory() {
		t.Errorf("expected directory first")
	}
	if items[1].Entry.Name != "orphan.txt" {
		t.Errorf("expected orphan second, got %q", items[1].Entry.Name)
	}
}

func TestBuildRemoteTree_ChildGroupedUnderDirectory(t *testing.T) {
	dir := RemoteFileEntry{Name: "mydir", EntryType: "directory", Size: 2048}
	child := RemoteFileEntry{Name: "mydir/child.txt", Size: 1024}
	orphan := RemoteFileEntry{Name: "orphan.txt", Size: 512}

	items := buildRemoteTree([]RemoteFileEntry{orphan, child, dir})

	if len(items) != 3 {
		t.Fatalf("expected 3, got %d", len(items))
	}
	if items[1].Entry.Name != "mydir/child.txt" {
		t.Errorf("expected child at [1], got %q", items[1].Entry.Name)
	}
	if items[1].Prefix == "" {
		t.Errorf("child should have indent prefix")
	}
	if items[2].Entry.Name != "orphan.txt" {
		t.Errorf("expected orphan at [2], got %q", items[2].Entry.Name)
	}
}

func TestBuildRemoteTree_LastChildGetsCornerPrefix(t *testing.T) {
	dir := RemoteFileEntry{Name: "d", EntryType: "directory", Size: 500}
	c1 := RemoteFileEntry{Name: "d/a", Size: 200}
	c2 := RemoteFileEntry{Name: "d/b", Size: 100}

	items := buildRemoteTree([]RemoteFileEntry{dir, c2, c1})
	// items: [dir, c1(200), c2(100)]
	if items[1].Prefix != "  ├─ " {
		t.Errorf("non-last prefix: got %q, want %q", items[1].Prefix, "  ├─ ")
	}
	if items[2].Prefix != "  └─ " {
		t.Errorf("last prefix: got %q, want %q", items[2].Prefix, "  └─ ")
	}
}

func TestBuildRemoteTree_SequentialIndices(t *testing.T) {
	dir := RemoteFileEntry{Name: "d", EntryType: "directory", Size: 500}
	child := RemoteFileEntry{Name: "d/c", Size: 200}
	orphan := RemoteFileEntry{Name: "f.txt", Size: 50}

	items := buildRemoteTree([]RemoteFileEntry{orphan, child, dir})

	for i, it := range items {
		if it.Idx != i {
			t.Errorf("item[%d].Idx = %d, want %d", i, it.Idx, i)
		}
	}
}
