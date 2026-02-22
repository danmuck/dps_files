package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
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
			hash, err := client.Upload(childPath)
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

func verifyChunks(t tui.TUI, ks *key_store.KeyStore, file *key_store.File) error {
	logs.Printf("\nVerifying stored chunks: %d\n", len(file.References))

	for i, ref := range file.References {
		if ref == nil {
			return fmt.Errorf("chunk reference %d is nil", i)
		}

		chunkData, err := ks.LoadFileReferenceData(ref.Key)
		if err != nil {
			return fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		if uint32(len(chunkData)) != ref.Size {
			return fmt.Errorf("chunk %d size mismatch: got %d, expected %d", i, len(chunkData), ref.Size)
		}

		dataHash := sha256.Sum256(chunkData)
		if dataHash != ref.DataHash {
			return fmt.Errorf("chunk %d hash mismatch:\nstored: %x\ncomputed: %x", i, ref.DataHash, dataHash)
		}

		if i%500 == 0 || i == int(file.MetaData.TotalBlocks-1) {
			t.FieldFU(fmt.Sprintf("Verified chunk %d/%d", i, file.MetaData.TotalBlocks-1),
				fmt.Sprintf("size=%d index=%d hash=%x", len(chunkData), ref.FileIndex, dataHash))
			logs.Printf("\n")
		}
	}

	return nil
}

func executeStoreTargets(cfg RuntimeConfig, ks *key_store.KeyStore, filePaths []string) error {
	t := cfg.TUI
	for _, sourcePath := range filePaths {
		displayName := filepath.Base(sourcePath)

		summary := OpSummary{
			Operation: "local-store",
			FileName:  displayName,
			Timer:     tui.NewPhaseTimer(),
			StartedAt: time.Now(),
		}
		if cfg.Mode == ModeRemote {
			summary.Operation = "remote-upload"
		}
		phaseTotal := 2
		if cfg.Mode == ModeRun {
			phaseTotal = 3
			if cfg.ReassembleEnabled {
				phaseTotal = 5
			}
		}
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

		var file *key_store.File
		switch cfg.Mode {
		case ModeRun:
			// Phase: chunk+store
			startPhase("chunk+store", "chunk and store local blocks")
			file, err = ks.LoadAndStoreFileLocal(sourcePath)
			summary.Timer.End()

		case ModeRemote:
			if cfg.RemoteAddr == "" {
				summary.Err = fmt.Errorf("remote mode requires an address")
				renderSummary(t, summary)
				writeOpLog(summary)
				return fmt.Errorf("remote mode requires an address; use %s or toggle mode in the menu", REMOTE_ADDR_FLAG)
			}
			client, dialErr := NewGRPCClient(cfg.RemoteAddr)
			if dialErr != nil {
				summary.Err = dialErr
				renderSummary(t, summary)
				writeOpLog(summary)
				return fmt.Errorf("connect to remote: %w", dialErr)
			}

			startPhase("upload", "upload file bytes to remote server")
			hash, uploadErr := client.Upload(sourcePath)
			client.Close()
			summary.Timer.End()

			summary.Bytes = sourceSize
			if uploadErr != nil {
				summary.Err = uploadErr
				renderSummary(t, summary)
				writeOpLog(summary)
				return fmt.Errorf("remote upload %s: %w", sourcePath, uploadErr)
			}
			logs.Printf("Remote upload complete. Server hash: %x\n", hash)
			renderSummary(t, summary)
			writeOpLog(summary)
			continue

		default:
			summary.Err = fmt.Errorf("unsupported mode %q", cfg.Mode)
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("unsupported mode %q", cfg.Mode)
		}

		if errors.Is(err, key_store.ErrFileHashCached) {
			logs.Printf("Skipping store for %q: %v\n", displayName, err)
			renderSummary(t, summary)
			writeOpLog(summary)
			continue
		}
		if err != nil {
			summary.Err = err
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to store file %s: %w", sourcePath, err)
		}

		summary.Bytes = file.MetaData.TotalSize

		logs.Printf("\n")
		t.MenuTitleTC(&tui.TitleParams{Text: "Stored metadata"})
		t.FieldFU("File name", file.MetaData.FileName); logs.Printf("\n")
		t.FieldFU("Total size", fmt.Sprintf("%d bytes", file.MetaData.TotalSize)); logs.Printf("\n")
		t.FieldFU("Chunk size", fmt.Sprintf("%d bytes", file.MetaData.BlockSize)); logs.Printf("\n")
		t.FieldFU("Total chunks", file.MetaData.TotalBlocks); logs.Printf("\n")
		if file.MetaData.TotalBlocks > 0 {
			t.FieldFU("Last chunk size", fmt.Sprintf("%d bytes",
				file.MetaData.TotalSize-uint64(file.MetaData.BlockSize*(file.MetaData.TotalBlocks-1))))
			logs.Printf("\n")
		}
		if len(file.References) > 0 {
			first := file.References[0]
			last := file.References[len(file.References)-1]
			t.FieldFU("First chunk", first.Location); logs.Printf("\n")
			t.FieldFU("Last chunk", last.Location); logs.Printf("\n")
		}

		if cfg.Mode != ModeRun {
			renderSummary(t, summary)
			writeOpLog(summary)
			continue
		}

		// Phase: verify
		startPhase("verify", "verify stored chunks")
		verifyErr := verifyChunks(t, ks, file)
		summary.Timer.End()
		if verifyErr != nil {
			summary.Err = verifyErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("chunk verification failed for %s: %w", sourcePath, verifyErr)
		}

		if !cfg.ReassembleEnabled {
			logs.Printf("Reassembly skipped (set %q to enable)\n", REASSEMBLE_FLAG)
			renderSummary(t, summary)
			writeOpLog(summary)
			continue
		}

		outputPath := copyOutputPath(cfg.KeyStore.StorageDir, displayName)
		if err := createDirPath(filepath.Dir(outputPath)); err != nil {
			summary.Err = err
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to ensure output directory: %w", err)
		}

		logs.Printf("\nReassembling file to: %s\n", outputPath)

		// Phase: reassemble
		startPhase("reassemble", "reassemble output file")
		reassembleErr := ks.ReassembleFileToPath(file.MetaData.FileHash, outputPath)
		summary.Timer.End()
		if reassembleErr != nil {
			summary.Err = reassembleErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to reassemble file %s: %w", sourcePath, reassembleErr)
		}

		// Phase: hash-check
		startPhase("hash-check", "hash-check reassembled output")
		reassembledHash, length, err := key_store.HashFile(outputPath)
		summary.Timer.End()
		if err != nil {
			summary.Err = err
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to verify reassembled file %s: %w", outputPath, err)
		}

		logs.Printf("\n")
		t.MenuTitleTC(&tui.TitleParams{Text: "Reassembly complete"})
		t.FieldFU("Original size", fmt.Sprintf("%d bytes", file.MetaData.TotalSize)); logs.Printf("\n")
		t.FieldFU("Original hash", fmt.Sprintf("%x", file.MetaData.FileHash)); logs.Printf("\n")
		t.FieldFU("Reassembled size", fmt.Sprintf("%d bytes", length)); logs.Printf("\n")
		t.FieldFU("Reassembled hash", fmt.Sprintf("%x", reassembledHash)); logs.Printf("\n")

		if file.MetaData.FileHash != reassembledHash {
			hashErr := fmt.Errorf("hash mismatch after reassembly for %s", sourcePath)
			summary.Err = hashErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return hashErr
		}

		logs.Printf("Successfully reassembled file to: %s\n", outputPath)
		renderSummary(t, summary)
		writeOpLog(summary)
	}

	return nil
}
