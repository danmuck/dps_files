package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"

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
	for _, sourcePath := range filePaths {
		displayName := filepath.Base(sourcePath)

		originalHash, originalSize, err := key_store.HashFile(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to hash source file %s: %w", sourcePath, err)
		}

		fmt.Printf("\nSource: %s\n", sourcePath)
		fmt.Printf("Original file size: %d bytes\n", originalSize)
		fmt.Printf("Original file hash: %x\n", originalHash)

		var file *key_store.File
		switch cfg.Mode {
		case ModeRun:
			file, err = ks.LoadAndStoreFileLocal(sourcePath)
		case ModeRemote:
			if cfg.RemoteAddr == "" {
				return fmt.Errorf("remote mode requires an address; use %s or toggle mode in the menu", REMOTE_ADDR_FLAG)
			}
			client := NewFileServerClient(cfg.RemoteAddr)
			client.Timeout = 0 // no deadline for large uploads
			hash, uploadErr := client.Upload(sourcePath)
			if uploadErr != nil {
				return fmt.Errorf("remote upload %s: %w", sourcePath, uploadErr)
			}
			fmt.Printf("Remote upload complete. Server hash: %x\n", hash)
			continue
		default:
			return fmt.Errorf("unsupported mode %q", cfg.Mode)
		}

		if errors.Is(err, key_store.ErrFileHashCached) {
			fmt.Printf("Skipping store for %q: %v\n", displayName, err)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to store file %s: %w", sourcePath, err)
		}

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
			continue
		}

		if err := verifyChunks(ks, file); err != nil {
			return fmt.Errorf("chunk verification failed for %s: %w", sourcePath, err)
		}

		if !cfg.ReassembleEnabled {
			fmt.Printf("Reassembly skipped (set %q to enable)\n", REASSEMBLE_FLAG)
			continue
		}

		outputPath := copyOutputPath(cfg.KeyStore.StorageDir, displayName)
		if err := createDirPath(filepath.Dir(outputPath)); err != nil {
			return fmt.Errorf("failed to ensure output directory: %w", err)
		}

		fmt.Printf("\nReassembling file to: %s\n", outputPath)
		if err := ks.ReassembleFileToPath(file.MetaData.FileHash, outputPath); err != nil {
			return fmt.Errorf("failed to reassemble file %s: %w", sourcePath, err)
		}

		reassembledHash, length, err := key_store.HashFile(outputPath)
		if err != nil {
			return fmt.Errorf("failed to verify reassembled file %s: %w", outputPath, err)
		}

		fmt.Printf("\nReassembly complete:\n")
		fmt.Printf("Original size: %d bytes\n", file.MetaData.TotalSize)
		fmt.Printf("Original hash: %x\n", file.MetaData.FileHash)
		fmt.Printf("Reassembled size: %d bytes\n", length)
		fmt.Printf("Reassembled hash: %x\n", reassembledHash)

		if file.MetaData.FileHash != reassembledHash {
			return fmt.Errorf("hash mismatch after reassembly for %s", sourcePath)
		}

		fmt.Printf("Successfully reassembled file to: %s\n", outputPath)
	}

	return nil
}
