package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

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

	logs.Titlef("\nSystem Stats\n")
	logs.DataKV("Generated at", time.Now().Format(time.RFC3339))
	logs.DataKV("Go version", runtimeStats.GoVersion)
	logs.Dataf("CPUs: %d  Goroutines: %d\n", runtimeStats.NumCPU, runtimeStats.NumGoroutine)
	logs.Dataf("Memory: alloc=%s  total_alloc=%s  sys=%s  num_gc=%d\n",
		formatBytes(runtimeStats.AllocBytes),
		formatBytes(runtimeStats.TotalAlloc),
		formatBytes(runtimeStats.SysBytes),
		runtimeStats.NumGC,
	)

	logs.Titlef("\nStorage Usage\n")
	logs.DataKV("Root", storageStats.RootPath)
	logs.DataKV("data/", formatBytes(storageStats.DataBytes))
	logs.DataKV("metadata/", formatBytes(storageStats.MetadataBytes))
	logs.DataKV(".cache/", formatBytes(storageStats.CacheBytes))
	logs.DataKV("other in storage/", formatBytes(storageStats.OtherBytes))
	logs.DataKV("total storage/", formatBytes(storageStats.TotalBytes))

	if cfg.Mode == ModeRemote && cfg.RemoteAddr != "" {
		logs.Titlef("\nRemote Server: %s\n", cfg.RemoteAddr)
		client := NewFileServerClient(cfg.RemoteAddr)
		entries, err := client.List()
		if err != nil {
			logs.Dataf("  Status: unreachable (%v)\n", err)
		} else {
			var totalSize uint64
			for _, e := range entries {
				totalSize += e.Size
			}
			logs.Dataf("  Status: reachable\n")
			logs.Dataf("  Files: %d  Total size: %s\n", len(entries), formatBytes(totalSize))
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
