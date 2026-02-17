package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DeepCleanResult struct {
	RemovedKDHT     int
	RemovedMetadata int
	RemovedCache    int
}

func createDirPath(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return nil
}

func getFilesInDirectory(dirPath string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(entry.Name()), "copy.") {
			continue
		}
		files = append(files, entry.Name())
	}

	return files, nil
}

func countKDHTFiles(storageDir string) (int, error) {
	pattern := filepath.Join(storageDir, "data", "*.kdht")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("failed to glob kdht files: %w", err)
	}
	return len(matches), nil
}

func cleanupCopyFiles(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(file.Name()), "copy.") {
			fullPath := filepath.Join(dir, file.Name())
			if err := os.Remove(fullPath); err != nil {
				return fmt.Errorf("failed to remove file %s: %w", fullPath, err)
			}
			fmt.Printf("Removed file: %s\n", fullPath)
		}
	}
	return nil
}

func cleanupAllKDHTFiles(storageDir string) (int, error) {
	pattern := filepath.Join(storageDir, "data", "*.kdht")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("failed to glob kdht files: %w", err)
	}

	removed := 0
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("failed to remove %s: %w", path, err)
		}
		removed++
	}
	return removed, nil
}

func countDirFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}

	return count, nil
}

func metadataFileCount(storageDir string) (int, error) {
	metadataDir := filepath.Join(storageDir, "metadata")
	entries, err := os.ReadDir(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read metadata directory %s: %w", metadataDir, err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".toml") {
			count++
		}
	}
	return count, nil
}

func deepCleanStorage(storageDir string) (DeepCleanResult, error) {
	result := DeepCleanResult{}

	removedKDHT, err := cleanupAllKDHTFiles(storageDir)
	if err != nil {
		return result, err
	}
	result.RemovedKDHT = removedKDHT

	metadataDir := filepath.Join(storageDir, "metadata")
	metadataCount, err := countDirFiles(metadataDir)
	if err != nil {
		return result, err
	}
	result.RemovedMetadata = metadataCount
	if err := os.RemoveAll(metadataDir); err != nil {
		return result, fmt.Errorf("failed to remove metadata directory: %w", err)
	}
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return result, fmt.Errorf("failed to recreate metadata directory: %w", err)
	}

	cacheDir := filepath.Join(storageDir, ".cache")
	cacheCount, err := countDirFiles(cacheDir)
	if err != nil {
		return result, err
	}
	result.RemovedCache = cacheCount
	if err := os.RemoveAll(cacheDir); err != nil {
		return result, fmt.Errorf("failed to remove cache directory: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return result, fmt.Errorf("failed to recreate cache directory: %w", err)
	}

	return result, nil
}

func copyOutputPath(storageDir, fileName string) string {
	return filepath.Join(storageDir, "copy."+filepath.Base(fileName))
}
