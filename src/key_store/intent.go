package key_store

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	logs "github.com/danmuck/smplog"
)

// intentRecord is the JSON structure written to .intents/{fileHash}.json.
// It captures enough context to identify and clean up orphaned chunks
// if a store operation crashes before metadata is persisted.
type intentRecord struct {
	FileHash    string `json:"file_hash"`
	FileName    string `json:"file_name"`
	TotalBlocks uint32 `json:"total_blocks"`
	BlockSize   uint32 `json:"block_size"`
	StartedAt   int64  `json:"started_at"`
}

func (ks *KeyStore) intentDir() string {
	return filepath.Join(ks.storageDir, ".intents")
}

// writeIntent creates an intent file before the chunk-writing loop begins.
// If the process crashes before clearIntent, recoverIntents will clean up.
func (ks *KeyStore) writeIntent(md MetaData) error {
	dir := ks.intentDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create intents directory: %w", err)
	}

	rec := intentRecord{
		FileHash:    fmt.Sprintf("%x", md.FileHash),
		FileName:    md.FileName,
		TotalBlocks: md.TotalBlocks,
		BlockSize:   md.BlockSize,
		StartedAt:   md.Modified,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal intent: %w", err)
	}

	finalPath := filepath.Join(dir, fmt.Sprintf("%x.json", md.FileHash))
	tmpFile, err := os.CreateTemp(dir, fmt.Sprintf("%x-*.tmp", md.FileHash))
	if err != nil {
		return fmt.Errorf("failed to create temp intent file: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp intent file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp intent file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to atomically publish intent file: %w", err)
	}
	cleanupTmp = false
	return nil
}

// clearIntent removes the intent file after metadata has been successfully persisted.
func (ks *KeyStore) clearIntent(fileHash [HashSize]byte) error {
	path := filepath.Join(ks.intentDir(), fmt.Sprintf("%x.json", fileHash))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear intent file: %w", err)
	}
	return nil
}

// recoverIntents scans the .intents/ directory on startup and cleans up
// orphaned chunk files from incomplete store operations.
func (ks *KeyStore) recoverIntents() error {
	dir := ks.intentDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read intents directory: %w", err)
	}

	var issues []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		intentPath := filepath.Join(dir, entry.Name())
		if err := ks.recoverIntentFile(intentPath); err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", entry.Name(), err))
			if ks.config.Verbose {
				logs.Warnf("intent recovery issue for %s: %v", entry.Name(), err)
			}
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("intent recovery encountered %d issue(s): %s", len(issues), strings.Join(issues, "; "))
	}
	return nil
}

func (ks *KeyStore) recoverIntentFile(intentPath string) error {
	data, err := os.ReadFile(intentPath)
	if err != nil {
		return fmt.Errorf("failed to read intent file: %w", err)
	}

	var rec intentRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		if rmErr := os.Remove(intentPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("failed to parse and remove corrupt intent: %w", rmErr)
		}
		return nil
	}

	fileHash, err := parseIntentHash(rec.FileHash)
	if err != nil {
		if rmErr := os.Remove(intentPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("invalid hash and failed removing intent: %w", rmErr)
		}
		return nil
	}

	metadataPath := filepath.Join(ks.storageDir, "metadata", fmt.Sprintf("%x.toml", fileHash))
	if _, err := os.Stat(metadataPath); err == nil {
		if ks.config.Verbose {
			logs.Infof("Intent recovery: skipping cleanup for committed file %s (%s)", rec.FileName, rec.FileHash)
		}
		if err := os.Remove(intentPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale intent: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check metadata for intent: %w", err)
	}

	cleaned := 0
	for i := uint32(0); i < rec.TotalBlocks; i++ {
		key := computeChunkKey(fileHash, i)
		chunkPath := ks.GetLocalBlockLocation(key)
		if err := os.Remove(chunkPath); err == nil {
			cleaned++
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove orphaned chunk %s: %w", chunkPath, err)
		}
	}

	if ks.config.Verbose && cleaned > 0 {
		logs.Infof("Intent recovery: cleaned %d orphaned chunks for %s (%s)", cleaned, rec.FileName, rec.FileHash)
	}

	if err := os.Remove(intentPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove recovered intent file: %w", err)
	}
	return nil
}

func parseIntentHash(hashHex string) ([HashSize]byte, error) {
	var out [HashSize]byte

	decoded, err := hex.DecodeString(hashHex)
	if err != nil {
		return out, fmt.Errorf("hex decode failed: %w", err)
	}
	if len(decoded) != HashSize {
		return out, fmt.Errorf("invalid hash length: got %d bytes, expected %d", len(decoded), HashSize)
	}
	copy(out[:], decoded)
	return out, nil
}
