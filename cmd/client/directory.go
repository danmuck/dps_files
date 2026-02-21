package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func executeUploadDirAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	reader := getBufferedReader(input)

	logs.Prompt("\nEnter directory path to upload: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read path: %w", err)
	}
	dirPath := strings.TrimSpace(line)
	if strings.EqualFold(dirPath, "e") {
		return errMenuBack
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dirPath)
	}

	logs.Printf("\nUploading directory %q...\n", dirPath)
	dirHash, err := ks.StoreDirectory(dirPath)
	if err != nil {
		return fmt.Errorf("store directory: %w", err)
	}
	logs.Printf("Directory stored. Root hash: %x\n", dirHash)
	return nil
}
