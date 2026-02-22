# Delete Parent-Check + Tree View Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** (1) Warn before deleting a file that belongs to a directory manifest so the user knows reassembly will break; (2) display all file-picker menus (view/download/delete, local and remote) as a sorted tree — directories first by size descending, with indented children grouped below, then orphan files.

**Architecture:** New `cmd/client/treeview.go` holds two pure tree-building functions (`buildLocalTree`, `buildRemoteTree`) that convert a flat metadata slice into a flat-but-visually-hierarchical list with sequential indices. Each of the six affected menus replaces its existing sort+render block with a call to the appropriate builder. Delete menus add a parent-check after index selection and prompt for confirmation before proceeding.

**Tech Stack:** Go stdlib (`sort`, `strings`), `key_store.MetaData`, `RemoteFileEntry` (already in package), `smplog` output helpers.

---

### Task 1: Create `treeview.go` + failing tests for local tree

**Files:**
- Create: `cmd/client/treeview.go`
- Create: `cmd/client/treeview_test.go`

**Step 1: Write the failing tests**

Create `cmd/client/treeview_test.go` with the following content:

```go
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
```

**Step 2: Run tests to confirm they fail**

```sh
cd "/Users/danmuck/Library/Mobile Documents/com~apple~CloudDocs/projects/suite/dps_files"
go test ./cmd/client/... -run "TestBuild" -v
```

Expected: FAIL — `buildLocalTree` and `buildRemoteTree` undefined.

---

### Task 2: Implement `treeview.go`

**Files:**
- Modify: `cmd/client/treeview.go` (implement functions)

**Step 1: Write the implementation**

Create `cmd/client/treeview.go`:

```go
package main

import (
	"sort"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

// localTreeItem is one row in the flat-indexed tree display for local metadata.
// Prefix is "" for root entries and "  ├─ " / "  └─ " for directory children.
type localTreeItem struct {
	Idx    int
	MD     key_store.MetaData
	Prefix string
}

// buildLocalTree groups metadata into a visual tree:
//   - directories sorted by ContentSize desc (fallback TotalSize), each immediately
//     followed by its children sorted by TotalSize desc
//   - orphan files (no ParentHash, not a directory) sorted by TotalSize desc
//
// Returns a flat sequentially-indexed list suitable for menu display and selection.
func buildLocalTree(metadata []key_store.MetaData) []localTreeItem {
	var zero [key_store.HashSize]byte
	var dirs, orphans []key_store.MetaData
	childrenOf := make(map[[key_store.HashSize]byte][]key_store.MetaData)

	for _, md := range metadata {
		switch {
		case md.IsDirectory():
			dirs = append(dirs, md)
		case md.ParentHash != zero:
			childrenOf[md.ParentHash] = append(childrenOf[md.ParentHash], md)
		default:
			orphans = append(orphans, md)
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		si := dirs[i].ContentSize
		if si == 0 {
			si = dirs[i].TotalSize
		}
		sj := dirs[j].ContentSize
		if sj == 0 {
			sj = dirs[j].TotalSize
		}
		return si > sj
	})

	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].TotalSize > orphans[j].TotalSize
	})

	var items []localTreeItem
	idx := 0

	for _, dir := range dirs {
		items = append(items, localTreeItem{Idx: idx, MD: dir, Prefix: ""})
		idx++

		children := append([]key_store.MetaData(nil), childrenOf[dir.FileHash]...)
		sort.Slice(children, func(i, j int) bool {
			return children[i].TotalSize > children[j].TotalSize
		})
		for ci, child := range children {
			prefix := "  ├─ "
			if ci == len(children)-1 {
				prefix = "  └─ "
			}
			items = append(items, localTreeItem{Idx: idx, MD: child, Prefix: prefix})
			idx++
		}
	}

	for _, f := range orphans {
		items = append(items, localTreeItem{Idx: idx, MD: f, Prefix: ""})
		idx++
	}

	return items
}

// remoteTreeItem is one row in the flat-indexed tree display for remote entries.
type remoteTreeItem struct {
	Idx    int
	Entry  RemoteFileEntry
	Prefix string
}

// buildRemoteTree groups remote entries into a visual tree using name-prefix
// matching: a non-directory entry whose Name starts with dirName+"/" is treated
// as a child of that directory.
//
// Ordering: directories (Size desc) with children (Size desc) grouped below,
// then orphan files (Size desc).
func buildRemoteTree(entries []RemoteFileEntry) []remoteTreeItem {
	var dirs []RemoteFileEntry
	for _, e := range entries {
		if e.IsDirectory() {
			dirs = append(dirs, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Size > dirs[j].Size })

	assigned := make(map[string]bool)
	var items []remoteTreeItem
	idx := 0

	for _, dir := range dirs {
		items = append(items, remoteTreeItem{Idx: idx, Entry: dir, Prefix: ""})
		idx++

		var children []RemoteFileEntry
		for _, e := range entries {
			if !e.IsDirectory() && strings.HasPrefix(e.Name, dir.Name+"/") {
				children = append(children, e)
				assigned[e.Name] = true
			}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Size > children[j].Size })
		for ci, child := range children {
			prefix := "  ├─ "
			if ci == len(children)-1 {
				prefix = "  └─ "
			}
			items = append(items, remoteTreeItem{Idx: idx, Entry: child, Prefix: prefix})
			idx++
		}
	}

	var orphans []RemoteFileEntry
	for _, e := range entries {
		if !e.IsDirectory() && !assigned[e.Name] {
			orphans = append(orphans, e)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Size > orphans[j].Size })
	for _, f := range orphans {
		items = append(items, remoteTreeItem{Idx: idx, Entry: f, Prefix: ""})
		idx++
	}

	return items
}
```

**Step 2: Run tests to confirm they pass**

```sh
go test ./cmd/client/... -run "TestBuild" -v
```

Expected: all `TestBuild*` tests PASS.

**Step 3: Run full suite to confirm no regressions**

```sh
make test
```

Expected: all tests pass.

**Step 4: Commit**

```sh
git add cmd/client/treeview.go cmd/client/treeview_test.go
git commit -m "feat(client): add tree-building helpers for file list menus"
```

---

### Task 3: Update `delete.go` — tree layout + parent-check (local mode)

**Files:**
- Modify: `cmd/client/delete.go`

**Context:** `executeDeleteAction` currently sorts `metadata` alphabetically and renders a flat list. Replace with `buildLocalTree`, then add a parent-check warning after the user picks an index.

**Step 1: Replace the local delete list+render block**

In `executeDeleteAction`, replace everything from `sort.Slice(metadata, ...)` through the closing `}` of the render loop with:

```go
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
```

**Step 2: Replace the selection + delete block with parent-check**

Replace the existing `reader := getBufferedReader(input)` loop block with:

```go
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
```

**Step 3: Remove unused `sort` import; ensure `key_store` and `strings` are imported**

After the edit, the import block should be:

```go
import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)
```

**Step 4: Build to check for errors**

```sh
go build ./cmd/client/...
```

Expected: no errors.

**Step 5: Commit**

```sh
git add cmd/client/delete.go
git commit -m "feat(client): tree layout + parent-check warning in local delete menu"
```

---

### Task 4: Update `delete.go` — tree layout + parent-check (remote mode)

**Files:**
- Modify: `cmd/client/delete.go`

**Context:** `executeRemoteDeleteAction` currently sorts entries alphabetically and renders a flat list. Replace with `buildRemoteTree` and add a name-heuristic parent-check.

**Step 1: Replace the remote delete list+render block**

In `executeRemoteDeleteAction`, replace from `sort.Slice(entries, ...)` through the closing `}` of the render loop with:

```go
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
```

**Step 2: Replace the selection + delete block with parent-check**

Replace the existing `reader := getBufferedReader(input)` loop with:

```go
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
```

**Step 3: Build**

```sh
go build ./cmd/client/...
```

Expected: no errors.

**Step 4: Commit**

```sh
git add cmd/client/delete.go
git commit -m "feat(client): tree layout + parent-check warning in remote delete menu"
```

---

### Task 5: Update `stream.go` — tree layout in download menus

**Files:**
- Modify: `cmd/client/stream.go`

**Context:** Both `executeDownloadAction` (local) and `executeRemoteDownloadAction` (remote) sort alphabetically and render flat lists. Replace with tree builders.

**Step 1: Update local download (`executeDownloadAction`)**

Replace from `sort.Slice(metadata, ...)` through the closing `}` of the render loop with:

```go
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
		logs.MenuItem(it.Idx, displayName+"  hash: "+shortHash+"...  chunks: "+fmt.Sprintf("%d", it.MD.TotalBlocks)+"  size: "+formatBytes(it.MD.TotalSize), false)
		logs.Printf("\n")
	}
```

Then update the selection block: replace `var selectedMD key_store.MetaData` and its loop with:

```go
	var selectedItem localTreeItem
	for {
		logs.Promptf("\nSelect file to download [0-%d] (or e to cancel): ", len(items)-1)
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
		selectedItem = items[idx]
		break
	}
```

Replace all subsequent references to `selectedMD` with `selectedItem.MD`.

**Step 2: Update remote download (`executeRemoteDownloadAction`)**

Replace from `sort.Slice(entries, ...)` through the render loop closing `}` with:

```go
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
```

Update the selection: replace `var selected RemoteFileEntry` and its loop with:

```go
	var selectedItem remoteTreeItem
	for {
		logs.Promptf("\nSelect file to download [0-%d] (or e to cancel): ", len(items)-1)
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
		selectedItem = items[idx]
		break
	}
```

Replace all subsequent references to `selected` with `selectedItem.Entry`.

**Step 3: Remove unused `sort` import from stream.go if no longer used elsewhere in the file**

Check: `sort` is only used in the two replaced blocks — remove it from the import block.

**Step 4: Build**

```sh
go build ./cmd/client/...
```

Expected: no errors.

**Step 5: Commit**

```sh
git add cmd/client/stream.go
git commit -m "feat(client): tree layout in local and remote download menus"
```

---

### Task 6: Update `view.go` — tree layout in view menus

**Files:**
- Modify: `cmd/client/view.go`

**Context:** `executeViewAction` (local) renders a flat list then calls `promptMetadataReassemblySelection(metadata, ...)`. `executeRemoteViewAction` (remote) renders a flat list. Both need tree layout.

**Step 1: Update local view (`executeViewAction`)**

Replace from `sort.Slice(metadata, ...)` through the closing `}` of the render loop with:

```go
	items := buildLocalTree(metadata)
	logs.Titlef("\nStored metadata entries (%d):\n", len(items))
	for _, it := range items {
		lastChunk := calculateLastChunkSize(it.MD)
		chunkSize := uint64(it.MD.BlockSize)
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
		logs.MenuItem(it.Idx, displayName, false)
		logs.Printf("\n")
		logs.Dataf("      hash: %s...  size: %s  chunks: %d\n", shortHash, formatBytes(displaySize), it.MD.TotalBlocks)
		logs.Dataf("      chunk_size: %s  last_chunk: %s  modified: %s  ttl: %s\n",
			formatBytes(chunkSize),
			formatBytes(lastChunk),
			formatUnixNano(it.MD.Modified),
			formatTTLSeconds(it.MD.TTL),
		)
	}
```

After the render loop, build an ordered slice from the tree to keep `promptMetadataReassemblySelection` indices aligned with what was displayed:

```go
	orderedMDs := make([]key_store.MetaData, len(items))
	for i, it := range items {
		orderedMDs[i] = it.MD
	}
	selected, selection, err := promptMetadataReassemblySelection(orderedMDs, input)
```

(Replace the old `promptMetadataReassemblySelection(metadata, input)` call.)

**Step 2: Update remote view (`executeRemoteViewAction`)**

Replace from `sort.Slice(entries, ...)` through the render loop closing `}` with:

```go
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
		logs.MenuItem(it.Idx, logs.PadRight(30, displayName)+"  hash: "+shortHash+"...  size: "+formatBytes(it.Entry.Size), false)
		logs.Printf("\n")
	}
```

**Step 3: Remove unused `sort` import from view.go if no longer used elsewhere**

**Step 4: Build**

```sh
go build ./cmd/client/...
```

Expected: no errors.

**Step 5: Run full test suite**

```sh
make test
```

Expected: all tests pass.

**Step 6: Commit**

```sh
git add cmd/client/view.go
git commit -m "feat(client): tree layout in local and remote view menus"
```

---

### Task 7: Final verification

**Step 1: Run all tests**

```sh
make test
```

Expected: PASS.

**Step 2: Build all commands**

```sh
make build
```

Expected: no errors.

**Step 3: Smoke-test local delete parent-check (optional manual)**

```sh
make client ARGS="--mode local --storage local/storage"
# Upload a directory, then attempt to delete one of its child files.
# Confirm the warning appears and that cancelling aborts the delete.
```
