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
