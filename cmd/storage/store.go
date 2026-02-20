package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
)

func verifyChunks(ks *key_store.KeyStore, file *key_store.File) error {
	fmt.Printf("\nVerifying stored chunks: %d\n", len(file.References))

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

		if i%key_store.PRINT_BLOCKS == 0 || i == int(file.MetaData.TotalBlocks-1) {
			fmt.Printf("Verified chunk %d/%d: size=%d index=%d hash=%x\n",
				i, file.MetaData.TotalBlocks-1, len(chunkData), ref.FileIndex, dataHash)
		}
	}

	return nil
}

func executeStoreTargets(cfg RuntimeConfig, ks *key_store.KeyStore, filePaths []string) error {
	showBar := !cfg.KeyStore.Verbose

	for _, sourcePath := range filePaths {
		displayName := filepath.Base(sourcePath)

		summary := OpSummary{
			Operation: "local-store",
			FileName:  displayName,
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
			beginPhase(&summary.Timer, summary.Operation, phaseName, stageLabel, phaseIndex, phaseTotal)
		}

		// Phase: hash
		startPhase("hash", "hash source file")
		originalHash, originalSize, err := key_store.HashFile(sourcePath)
		summary.Timer.Stop(err != nil)
		if err != nil {
			summary.Err = err
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to hash source file %s: %w", sourcePath, err)
		}
		if originalSize < 0 {
			sizeErr := fmt.Errorf("negative source file size for %s", sourcePath)
			summary.Err = sizeErr
			renderSummary(summary)
			writeOpLog(summary)
			return sizeErr
		}
		sourceSize := uint64(originalSize)
		summary.FileSize = sourceSize

		fmt.Printf("\nSource: %s\n", sourcePath)
		fmt.Printf("Original file size: %d bytes\n", originalSize)
		fmt.Printf("Original file hash: %x\n", originalHash)

		var file *key_store.File
		switch cfg.Mode {
		case ModeRun:
			// Phase: chunk+store
			startPhase("chunk+store", "chunk and store local blocks")
			file, err = ks.LoadAndStoreFileLocal(sourcePath)
			summary.Timer.Stop(err != nil)

		case ModeRemote:
			if cfg.RemoteAddr == "" {
				summary.Err = fmt.Errorf("remote mode requires an address")
				renderSummary(summary)
				writeOpLog(summary)
				return fmt.Errorf("remote mode requires an address; use %s or toggle mode in the menu", REMOTE_ADDR_FLAG)
			}
			f, openErr := os.Open(sourcePath)
			if openErr != nil {
				summary.Err = openErr
				renderSummary(summary)
				writeOpLog(summary)
				return fmt.Errorf("open %s for upload: %w", sourcePath, openErr)
			}

			pr := newProgressReader(f, sourceSize, "upload", showBar)
			client := NewFileServerClient(cfg.RemoteAddr)
			client.Timeout = 0 // no deadline for large uploads

			startPhase("upload", "upload file bytes to remote server")
			hash, uploadErr := client.Upload(sourcePath, pr)
			pr.Finish()
			f.Close()
			summary.Timer.Stop(uploadErr != nil)

			summary.Bytes = pr.BytesRead()
			if uploadErr != nil {
				summary.Err = uploadErr
				renderSummary(summary)
				writeOpLog(summary)
				return fmt.Errorf("remote upload %s: %w", sourcePath, uploadErr)
			}
			fmt.Printf("Remote upload complete. Server hash: %x\n", hash)
			renderSummary(summary)
			writeOpLog(summary)
			continue

		default:
			summary.Err = fmt.Errorf("unsupported mode %q", cfg.Mode)
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("unsupported mode %q", cfg.Mode)
		}

		if errors.Is(err, key_store.ErrFileHashCached) {
			fmt.Printf("Skipping store for %q: %v\n", displayName, err)
			renderSummary(summary)
			writeOpLog(summary)
			continue
		}
		if err != nil {
			summary.Err = err
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to store file %s: %w", sourcePath, err)
		}

		summary.Bytes = file.MetaData.TotalSize

		fmt.Printf("\nStored metadata:\n")
		fmt.Printf("File name: %s\n", file.MetaData.FileName)
		fmt.Printf("Total size: %d bytes\n", file.MetaData.TotalSize)
		fmt.Printf("Chunk size: %d bytes\n", file.MetaData.BlockSize)
		fmt.Printf("Total chunks: %d\n", file.MetaData.TotalBlocks)
		if file.MetaData.TotalBlocks > 0 {
			fmt.Printf("Last chunk size: %d bytes\n",
				file.MetaData.TotalSize-uint64(file.MetaData.BlockSize*(file.MetaData.TotalBlocks-1)))
		}
		if len(file.References) > 0 {
			first := file.References[0]
			last := file.References[len(file.References)-1]
			fmt.Printf("Chunk files written:\n")
			fmt.Printf("  first: %s\n", first.Location)
			fmt.Printf("  last:  %s\n", last.Location)
		}

		if cfg.Mode != ModeRun {
			renderSummary(summary)
			writeOpLog(summary)
			continue
		}

		// Phase: verify
		startPhase("verify", "verify stored chunks")
		verifyErr := verifyChunks(ks, file)
		summary.Timer.Stop(verifyErr != nil)
		if verifyErr != nil {
			summary.Err = verifyErr
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("chunk verification failed for %s: %w", sourcePath, verifyErr)
		}

		if !cfg.ReassembleEnabled {
			fmt.Printf("Reassembly skipped (set %q to enable)\n", REASSEMBLE_FLAG)
			renderSummary(summary)
			writeOpLog(summary)
			continue
		}

		outputPath := copyOutputPath(cfg.KeyStore.StorageDir, displayName)
		if err := createDirPath(filepath.Dir(outputPath)); err != nil {
			summary.Err = err
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to ensure output directory: %w", err)
		}

		fmt.Printf("\nReassembling file to: %s\n", outputPath)

		// Phase: reassemble
		startPhase("reassemble", "reassemble output file")
		reassembleErr := ks.ReassembleFileToPath(file.MetaData.FileHash, outputPath)
		summary.Timer.Stop(reassembleErr != nil)
		if reassembleErr != nil {
			summary.Err = reassembleErr
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to reassemble file %s: %w", sourcePath, reassembleErr)
		}

		// Phase: hash-check
		startPhase("hash-check", "hash-check reassembled output")
		reassembledHash, length, err := key_store.HashFile(outputPath)
		summary.Timer.Stop(err != nil)
		if err != nil {
			summary.Err = err
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("failed to verify reassembled file %s: %w", outputPath, err)
		}

		fmt.Printf("\nReassembly complete:\n")
		fmt.Printf("Original size: %d bytes\n", file.MetaData.TotalSize)
		fmt.Printf("Original hash: %x\n", file.MetaData.FileHash)
		fmt.Printf("Reassembled size: %d bytes\n", length)
		fmt.Printf("Reassembled hash: %x\n", reassembledHash)

		if file.MetaData.FileHash != reassembledHash {
			hashErr := fmt.Errorf("hash mismatch after reassembly for %s", sourcePath)
			summary.Err = hashErr
			renderSummary(summary)
			writeOpLog(summary)
			return hashErr
		}

		fmt.Printf("Successfully reassembled file to: %s\n", outputPath)
		renderSummary(summary)
		writeOpLog(summary)
	}

	return nil
}
