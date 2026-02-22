package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

type RuntimeStats struct {
	GoVersion    string
	NumCPU       int
	NumGoroutine int
	AllocBytes   uint64
	TotalAlloc   uint64
	SysBytes     uint64
	NumGC        uint32
}

type StorageStats struct {
	RootPath      string
	DataBytes     uint64
	MetadataBytes uint64
	CacheBytes    uint64
	OtherBytes    uint64
	TotalBytes    uint64
}

func executeStatsAction(cfg RuntimeConfig) error {
	runtimeStats := collectRuntimeStats()
	storageStats, err := collectStorageStats(cfg.KeyStore.StorageDir)
	if err != nil {
		return err
	}

	t := cfg.TUI
	nl := func() { logs.Printf("\n") }

	t.MenuTitleTC(&tui.TitleParams{Text: "System Stats"})
	t.FieldFU("Generated at", time.Now().Format(time.RFC3339)); nl()
	t.FieldFU("Go version", runtimeStats.GoVersion); nl()
	t.FieldFU("CPUs", runtimeStats.NumCPU); nl()
	t.FieldFU("Goroutines", runtimeStats.NumGoroutine); nl()
	t.FieldFU("Memory alloc", formatBytes(runtimeStats.AllocBytes)); nl()
	t.FieldFU("Memory total_alloc", formatBytes(runtimeStats.TotalAlloc)); nl()
	t.FieldFU("Memory sys", formatBytes(runtimeStats.SysBytes)); nl()
	t.FieldFU("GC cycles", runtimeStats.NumGC); nl()

	t.MenuTitleTC(&tui.TitleParams{Text: "Storage Usage"})
	t.FieldFU("Root", storageStats.RootPath); nl()
	t.FieldFU("data/", formatBytes(storageStats.DataBytes)); nl()
	t.FieldFU("metadata/", formatBytes(storageStats.MetadataBytes)); nl()
	t.FieldFU(".cache/", formatBytes(storageStats.CacheBytes)); nl()
	t.FieldFU("other in storage/", formatBytes(storageStats.OtherBytes)); nl()
	t.FieldFU("total storage/", formatBytes(storageStats.TotalBytes)); nl()

	if cfg.Mode == ModeRemote && cfg.RemoteAddr != "" {
		t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Remote Server: %s", cfg.RemoteAddr)})
		client, dialErr := NewGRPCClient(cfg.RemoteAddr)
		if dialErr != nil {
			t.StatusErrorFU(fmt.Sprintf("Status: unreachable (%v)", dialErr)); nl()
		} else {
			defer client.Close()
			rs, statsErr := client.RemoteStorageStats()
			if statsErr != nil {
				t.StatusErrorFU(fmt.Sprintf("Status: unreachable (%v)", statsErr)); nl()
			} else {
				t.StatusInfoFU("Status: reachable"); nl()
				t.FieldFU("Files", rs.FileCount); nl()
				t.FieldFU("data/", formatBytes(rs.DataBytes)); nl()
				t.FieldFU("metadata/", formatBytes(rs.MetadataBytes)); nl()
				t.FieldFU(".cache/", formatBytes(rs.CacheBytes)); nl()
				t.FieldFU("total", formatBytes(rs.TotalBytes)); nl()
			}
		}
	}

	return nil
}

func collectRuntimeStats() RuntimeStats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return RuntimeStats{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		AllocBytes:   mem.Alloc,
		TotalAlloc:   mem.TotalAlloc,
		SysBytes:     mem.Sys,
		NumGC:        mem.NumGC,
	}
}

func collectStorageStats(storageDir string) (StorageStats, error) {
	stats := StorageStats{
		RootPath: filepath.Clean(storageDir),
	}

	entries, err := os.ReadDir(storageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, fmt.Errorf("failed to read storage root %s: %w", storageDir, err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(storageDir, entry.Name())
		size, err := pathSize(entryPath)
		if err != nil {
			return stats, err
		}

		switch entry.Name() {
		case "data":
			stats.DataBytes += size
		case "metadata":
			stats.MetadataBytes += size
		case ".cache":
			stats.CacheBytes += size
		default:
			stats.OtherBytes += size
		}
	}

	stats.TotalBytes = stats.DataBytes + stats.MetadataBytes + stats.CacheBytes + stats.OtherBytes
	return stats, nil
}

func pathSize(path string) (uint64, error) {
	var total uint64

	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Size() > 0 {
			total += uint64(info.Size())
		}

		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to scan %s: %w", path, err)
	}

	return total, nil
}
