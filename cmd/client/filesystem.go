package main

import (
	"fmt"
	"os"
	"strings"
)

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

// UploadEntry describes one item in the upload directory listing.
type UploadEntry struct {
	Name  string
	IsDir bool
}

// getUploadDirEntries lists all non-copy.* entries in dirPath,
// returning both files and subdirectory names, each tagged with IsDir.
func getUploadDirEntries(dirPath string) ([]UploadEntry, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dirPath, err)
	}
	var result []UploadEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "copy.") {
			continue
		}
		result = append(result, UploadEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return result, nil
}
