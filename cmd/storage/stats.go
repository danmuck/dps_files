package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
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

	fmt.Println("\nSystem Stats")
	fmt.Printf("Generated at: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Go version: %s\n", runtimeStats.GoVersion)
	fmt.Printf("CPUs: %d  Goroutines: %d\n", runtimeStats.NumCPU, runtimeStats.NumGoroutine)
	fmt.Printf("Memory: alloc=%s  total_alloc=%s  sys=%s  num_gc=%d\n",
		formatBytes(runtimeStats.AllocBytes),
		formatBytes(runtimeStats.TotalAlloc),
		formatBytes(runtimeStats.SysBytes),
		runtimeStats.NumGC,
	)

	fmt.Println("\nStorage Usage")
	fmt.Printf("Root: %s\n", storageStats.RootPath)
	fmt.Printf("data/: %s\n", formatBytes(storageStats.DataBytes))
	fmt.Printf("metadata/: %s\n", formatBytes(storageStats.MetadataBytes))
	fmt.Printf(".cache/: %s\n", formatBytes(storageStats.CacheBytes))
	fmt.Printf("other in storage/: %s\n", formatBytes(storageStats.OtherBytes))
	fmt.Printf("total storage/: %s\n", formatBytes(storageStats.TotalBytes))

	if cfg.Mode == ModeRemote && cfg.RemoteAddr != "" {
		fmt.Printf("\nRemote Server: %s\n", cfg.RemoteAddr)
		client := NewFileServerClient(cfg.RemoteAddr)
		entries, err := client.List()
		if err != nil {
			fmt.Printf("  Status: unreachable (%v)\n", err)
		} else {
			var totalSize uint64
			for _, e := range entries {
				totalSize += e.Size
			}
			fmt.Printf("  Status: reachable\n")
			fmt.Printf("  Files: %d  Total size: %s\n", len(entries), formatBytes(totalSize))
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
