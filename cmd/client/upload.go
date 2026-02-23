package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

// executeRemoteUploadDir recursively uploads a local directory to a remote server.
// It uploads each file via client.Upload, assembles a DirectoryManifest from the
// returned hashes, then sends the manifest via client.UploadDirManifest.
// Returns the root manifest hash and the combined content size of all files.
func executeRemoteUploadDir(client *GRPCClient, localPath, rootPath string) ([32]byte, uint64, error) {
	entries, err := os.ReadDir(localPath)
	if err != nil {
		return [32]byte{}, 0, fmt.Errorf("read dir %s: %w", localPath, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var children []key_store.DirectoryEntry
	var totalSize uint64
	for _, entry := range entries {
		childPath := filepath.Join(localPath, entry.Name())
		relPath, err := filepath.Rel(rootPath, childPath)
		if err != nil {
			return [32]byte{}, 0, fmt.Errorf("rel path: %w", err)
		}
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			subHash, subSize, err := executeRemoteUploadDir(client, childPath, rootPath)
			if err != nil {
				return [32]byte{}, 0, err
			}
			totalSize += subSize
			children = append(children, key_store.DirectoryEntry{
				Name: entry.Name(),
				Path: relPath,
				Hash: subHash,
				Type: "directory",
				Size: subSize,
			})
		} else {
			hash, err := client.Upload(childPath, relPath)
			if err != nil {
				return [32]byte{}, 0, fmt.Errorf("upload %s: %w", relPath, err)
			}
			info, err := entry.Info()
			if err != nil {
				return [32]byte{}, 0, fmt.Errorf("stat %s: %w", relPath, err)
			}
			fileSize := uint64(info.Size())
			totalSize += fileSize
			children = append(children, key_store.DirectoryEntry{
				Name: entry.Name(),
				Path: relPath,
				Hash: hash,
				Type: "file",
				Size: fileSize,
			})
		}
	}

	dirRelPath, err := filepath.Rel(rootPath, localPath)
	if err != nil {
		return [32]byte{}, 0, fmt.Errorf("rel path for dir: %w", err)
	}
	dirRelPath = filepath.ToSlash(dirRelPath)
	if dirRelPath == "." {
		dirRelPath = filepath.Base(rootPath)
	}

	manifest := key_store.DirectoryManifest{Path: dirRelPath, Children: children, TotalSize: totalSize}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return [32]byte{}, 0, fmt.Errorf("marshal manifest: %w", err)
	}
	hash, err := client.UploadDirManifest(manifestJSON)
	return hash, totalSize, err
}

func executeStoreTargets(cfg RuntimeConfig, client *GRPCClient, filePaths []string) error {
	t := cfg.TUI
	for _, sourcePath := range filePaths {
		displayName := filepath.Base(sourcePath)

		summary := OpSummary{
			Operation: "upload",
			FileName:  displayName,
			Timer:     tui.NewPhaseTimer(),
			StartedAt: time.Now(),
		}
		phaseTotal := 2
		phaseIndex := 0
		startPhase := func(phaseName, stageLabel string) {
			phaseIndex++
			beginPhase(summary.Timer, summary.Operation, phaseName, stageLabel, phaseIndex, phaseTotal)
		}

		// Phase: hash
		startPhase("hash", "hash source file")
		originalHash, originalSize, err := key_store.HashFile(sourcePath)
		summary.Timer.End()
		if err != nil {
			summary.Err = err
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to hash source file %s: %w", sourcePath, err)
		}
		if originalSize < 0 {
			sizeErr := fmt.Errorf("negative source file size for %s", sourcePath)
			summary.Err = sizeErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return sizeErr
		}
		sourceSize := uint64(originalSize)
		summary.FileSize = sourceSize

		logs.Printf("\n")
		t.FieldFU("Source", sourcePath); logs.Printf("\n")
		t.FieldFU("Original file size", fmt.Sprintf("%d bytes", originalSize)); logs.Printf("\n")
		t.FieldFU("Original file hash", fmt.Sprintf("%x", originalHash)); logs.Printf("\n")

		// Phase: upload
		startPhase("upload", "upload file bytes to server")
		hash, uploadErr := client.Upload(sourcePath, filepath.Base(sourcePath))
		summary.Timer.End()

		summary.Bytes = sourceSize
		if uploadErr != nil {
			summary.Err = uploadErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("upload %s: %w", sourcePath, uploadErr)
		}
		logs.Printf("Upload complete. Server hash: %x\n", hash)
		renderSummary(t, summary)
		writeOpLog(summary)
	}

	return nil
}
